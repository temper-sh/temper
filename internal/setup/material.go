package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/hfcache"
	"github.com/temper-sh/temper/internal/preset"
)

// Material is the read-only observation of exact artifact sets and reusable
// model files under one root and an optional shared HF cache. The inspection
// functions populate these facts without hashing multi-GB weights.
type Material struct {
	root     string
	verified map[string]bool
	models   map[string]artifactset.ModelFile // Model SHA-256 to admitted file.
	hfModels map[hfcache.Entry]cachedModel
	hfCache  *CachePlan
	software map[string]bool
}

func (m *Material) InspectPresetSoftware(ctx context.Context, locks []catalog.Lock) error {
	m.software = map[string]bool{}
	for _, lock := range locks {
		sets, err := preset.Software(lock)
		if err != nil {
			return err
		}
		for _, set := range sets {
			if _, seen := m.software[set.ID]; seen {
				continue
			}
			installed, err := preset.Installed(ctx, m.root, set)
			if err != nil {
				return err
			}
			m.software[set.ID] = installed
		}
	}
	return nil
}

type cachedModel struct {
	copy bool
}

// InspectModelsWithCache adds shared HF material to the Temper receipt lookup.
// Preview inspects paths and sizes only; fetch hashes staged bytes before use.
func InspectModelsWithCache(root string, locks []catalog.Lock, cacheRoot string) (Material, error) {
	material, err := InspectModels(root, locks)
	if err != nil || cacheRoot == "" {
		return material, err
	}
	cache := hfcache.Cache{Root: cacheRoot}
	shared, free, err := cache.Space(material.root)
	if err != nil {
		return Material{}, err
	}
	material.hfCache = &CachePlan{Root: cacheRoot, SharedFilesystem: shared, FreeDiskBytes: free}
	material.hfModels = make(map[hfcache.Entry]cachedModel)
	for _, locked := range locks {
		for _, artifact := range locked.Records.Artifacts {
			for _, file := range artifact.Files {
				if _, ok := material.models[file.SHA256]; ok {
					continue
				}
				entry := hfcache.Entry{Repo: artifact.Repo, Revision: artifact.Revision, Name: file.Path, SHA256: file.SHA256}
				if _, ok := material.hfModels[entry]; ok {
					continue
				}
				found, ok, err := cache.Inspect(entry)
				if err != nil {
					return Material{}, err
				}
				if !ok {
					continue
				}
				if found.Size != file.Bytes {
					return Material{}, fmt.Errorf("HF cache file %q size is %d; catalog requires %d", file.Path, found.Size, file.Bytes)
				}
				shared, err := hfcache.SameFilesystem(found.Path, material.root)
				if err != nil {
					return Material{}, err
				}
				material.hfModels[entry] = cachedModel{copy: !shared}
			}
		}
	}
	return material, nil
}

// InspectModels credits only complete, receipt-verified artifact sets. It does
// not hash model weights, download files, or create the root. A malformed
// existing set is a refusal, never an absent set.
func InspectModels(root string, locks []catalog.Lock) (Material, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return Material{}, err
	}
	if _, err := ExistingDirectory(resolved); err != nil {
		return Material{}, err
	}
	material := Material{root: resolved, verified: map[string]bool{}}
	var hashes []string
	for _, locked := range locks {
		if err := locked.Validate(); err != nil {
			return Material{}, err
		}
		for _, artifact := range locked.Records.Artifacts {
			for _, file := range artifact.Files {
				hashes = append(hashes, file.SHA256)
			}
		}
		projection, err := locked.Projections()
		if err != nil {
			return Material{}, err
		}
		for _, id := range keys(locked.Records.Layouts) {
			set, err := artifactset.New(resolved, id, projection.Manifest.Layouts[id], projection.Artifacts.Entries[id], projection.Manifest.Patches)
			if err != nil {
				return Material{}, err
			}
			if material.verified[set.Path()] {
				continue
			}
			absent, err := inspectSetParents(resolved, filepath.Dir(set.Path()))
			if err != nil {
				return Material{}, err
			}
			if absent {
				continue
			}
			if _, err := set.Inspect(); err != nil {
				if errors.Is(err, artifactset.ErrNotMaterialized) {
					continue
				}
				return Material{}, fmt.Errorf("inspect existing model set %q: %w", id, err)
			}
			material.verified[set.Path()] = true
		}
	}
	material.models, err = artifactset.FindModels(resolved, hashes)
	if err != nil {
		return Material{}, err
	}
	return material, nil
}

func inspectSetParents(root, parent string) (bool, error) {
	for path := root; ; {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("model-set parent %s must be a real directory", path)
		}
		if path == parent {
			return false, nil
		}
		relative, err := filepath.Rel(path, parent)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return false, errors.New("model-set path escapes the selected root")
		}
		path = filepath.Join(path, strings.Split(relative, string(filepath.Separator))[0])
	}
}
