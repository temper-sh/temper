package distribution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/datadir"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type pointer struct {
	Active string `json:"active"`
	Latest string `json:"latest"`
}

type store struct {
	root           string
	raw            []byte
	active, latest Snapshot
}

func readStore(root string, trust publication.TrustRoot) (store, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return store{}, err
	}
	s := store{root: resolved}
	for _, path := range s.directories() {
		if err := checkDirectory(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return store{}, err
		}
	}
	raw, err := readFile(s.statePath(), 1024)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return store{}, err
	}
	var state pointer
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&state); err != nil {
		return store{}, fmt.Errorf("read catalog state: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return store{}, errors.New("catalog state must contain one JSON object")
	}
	if !digestPattern.MatchString(state.Active) || !digestPattern.MatchString(state.Latest) {
		return store{}, errors.New("catalog state requires exact active and latest snapshot digests")
	}
	s.raw = raw
	s.active, err = s.readSnapshot(state.Active, trust)
	if err != nil {
		return store{}, fmt.Errorf("read active catalog: %w", err)
	}
	s.latest = s.active
	if state.Latest != state.Active {
		s.latest, err = s.readSnapshot(state.Latest, trust)
		if err != nil {
			return store{}, fmt.Errorf("read highest accepted catalog: %w", err)
		}
		if s.active.Sequence >= s.latest.Sequence {
			return store{}, errors.New("active catalog conflicts with the highest accepted publication")
		}
	}
	return s, nil
}

func (s store) catalogRoot() string   { return filepath.Join(s.root, "catalog") }
func (s store) snapshotsRoot() string { return filepath.Join(s.catalogRoot(), "snapshots") }
func (s store) statePath() string     { return filepath.Join(s.catalogRoot(), "state.json") }
func (s store) directories() []string { return []string{s.root, s.catalogRoot(), s.snapshotsRoot()} }

func (s store) readSnapshot(sha string, trust publication.TrustRoot) (Snapshot, error) {
	if !digestPattern.MatchString(sha) {
		return Snapshot{}, errors.New("snapshot must be an exact lowercase SHA-256 digest")
	}
	path := filepath.Join(s.snapshotsRoot(), sha)
	p, err := readPublication(path)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot, err := Verify(p, trust)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.SHA256 != sha {
		return Snapshot{}, errors.New("stored catalog does not match its directory digest")
	}
	return snapshot, nil
}

func (s store) snapshots(trust publication.TrustRoot) ([]Identity, error) {
	entries, err := os.ReadDir(s.snapshotsRoot())
	if err != nil {
		return nil, err
	}
	identities := []Identity{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".temper-catalog-snapshot-") {
			continue
		}
		snapshot, err := s.readSnapshot(entry.Name(), trust)
		if err != nil {
			return nil, fmt.Errorf("inspect retained snapshot %q: %w", entry.Name(), err)
		}
		// A fully staged snapshot whose state commit failed is safe to reuse on
		// retry, but is not yet an accepted rollback target above the high-water mark.
		if snapshot.Sequence > s.latest.Sequence || snapshot.Sequence == s.latest.Sequence && snapshot.SHA256 != s.latest.SHA256 {
			continue
		}
		identities = append(identities, snapshot.Identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		if identities[i].Sequence == identities[j].Sequence {
			return identities[i].SHA256 < identities[j].SHA256
		}
		return identities[i].Sequence > identities[j].Sequence
	})
	return identities, nil
}

func (s store) commit(ctx context.Context, active, latest Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, path := range s.directories() {
		if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := checkDirectory(path); err != nil {
			return err
		}
	}
	lock, err := lockStore(filepath.Join(s.catalogRoot(), "write.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := readFile(s.statePath(), 1024)
	if errors.Is(err, fs.ErrNotExist) {
		current = nil
	} else if err != nil {
		return err
	}
	if !bytes.Equal(current, s.raw) {
		return errors.New("catalog state changed concurrently; rerun command")
	}
	if err := s.storeSnapshot(active); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(pointer{Active: active.SHA256, Latest: latest.SHA256})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(s.catalogRoot(), ".temper-catalog-state-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := writeSynced(file, data); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), s.statePath()); err != nil {
		return fmt.Errorf("commit catalog state: %w", err)
	}
	if err := syncDirectory(s.catalogRoot()); err != nil {
		return fmt.Errorf("catalog state committed but directory sync failed: %w", err)
	}
	return nil
}

func (s store) storeSnapshot(snapshot Snapshot) error {
	path := filepath.Join(s.snapshotsRoot(), snapshot.SHA256)
	if _, err := os.Lstat(path); err == nil {
		stored, err := readPublication(path)
		if err != nil {
			return err
		}
		for name, data := range publicationFiles(snapshot.publication) {
			if !bytes.Equal(data, publicationFiles(stored)[name]) {
				return errors.New("immutable catalog snapshot differs from the verified publication")
			}
		}
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	stage, err := os.MkdirTemp(s.snapshotsRoot(), ".temper-catalog-snapshot-")
	if err != nil {
		return err
	}
	defer func() {
		for name := range publicationFiles(snapshot.publication) {
			_ = os.Remove(filepath.Join(stage, name))
		}
		_ = os.Remove(stage)
	}()
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	for name, data := range publicationFiles(snapshot.publication) {
		file, err := os.OpenFile(filepath.Join(stage, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		if err := writeSynced(file, data); err != nil {
			return err
		}
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	if err := os.Rename(stage, path); err != nil {
		return fmt.Errorf("store immutable catalog snapshot: %w", err)
	}
	return syncDirectory(s.snapshotsRoot())
}

func publicationFiles(p Publication) map[string][]byte {
	return map[string][]byte{"catalog.json": p.Catalog.Data, "catalog.signature.yaml": p.Catalog.Signature,
		"channel.yaml": p.Channel.Data, "channel.signature.yaml": p.Channel.Signature}
}

func readPublication(path string) (Publication, error) {
	if err := checkDirectory(path); err != nil {
		return Publication{}, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Publication{}, err
	}
	limits := map[string]int64{"catalog.json": catalogsource.MaxCatalogBytes, "catalog.signature.yaml": catalogsource.MaxSignatureBytes,
		"channel.yaml": catalogsource.MaxChannelBytes, "channel.signature.yaml": catalogsource.MaxSignatureBytes}
	if len(entries) != len(limits) {
		return Publication{}, errors.New("stored catalog must contain exactly its catalog, channel and detached signatures")
	}
	files := map[string][]byte{}
	for _, entry := range entries {
		limit, ok := limits[entry.Name()]
		if !ok {
			return Publication{}, fmt.Errorf("unexpected catalog snapshot file %q", entry.Name())
		}
		files[entry.Name()], err = readFile(filepath.Join(path, entry.Name()), limit)
		if err != nil {
			return Publication{}, err
		}
	}
	return Publication{Catalog: publication.SignedArtifact{Data: files["catalog.json"], Signature: files["catalog.signature.yaml"]},
		Channel: publication.SignedArtifact{Data: files["channel.yaml"], Signature: files["channel.signature.yaml"]}}, nil
}

// Inspect both the path and opened file, and bound reads even if the size changes.
func readFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("catalog file %q must be regular, not a directory or symlink", path)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("catalog file %q exceeds its size limit", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, opened) {
		return nil, errors.New("catalog file changed while opening; rerun command")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("catalog file %q exceeds its size limit", path)
	}
	return data, nil
}

func checkDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("catalog directory %q must be a real directory", path)
	}
	return nil
}

func writeSynced(file *os.File, data []byte) error {
	err := file.Chmod(0o644)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
