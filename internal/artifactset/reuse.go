package artifactset

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ModelFile is a regular file in an admitted, published artifact set. The size
// and hash come from its receipt; fetch must hash the bytes again before reuse.
type ModelFile struct {
	Path string
	Size int64
}

// FindModels locates selected model hashes across existing layout/template
// compositions. It reads receipts and file shapes, never model bytes. No cache
// index or second copy of model material is maintained.
func FindModels(root string, hashes []string) (map[string]ModelFile, error) {
	wanted := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		wanted[hash] = true
	}
	found := map[string]ModelFile{}
	if len(wanted) == 0 {
		return found, nil
	}
	base := filepath.Join(root, "artifacts", "layouts")
	for _, path := range []string{root, filepath.Join(root, "artifacts"), base} {
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return found, nil
		}
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("model-set parent %s must be a real directory", path)
		}
	}
	layouts, err := os.ReadDir(base)
	if err != nil {
		return nil, err
	}
	for _, layout := range layouts {
		if strings.HasPrefix(layout.Name(), ".") || !layout.IsDir() {
			continue
		}
		sets, err := os.ReadDir(filepath.Join(base, layout.Name()))
		if err != nil {
			return nil, err
		}
		for _, set := range sets {
			if !validDigest(set.Name()) {
				continue // A private stage is not published model material.
			}
			path := filepath.Join(base, layout.Name(), set.Name())
			recorded, err := readReceipt(path, layout.Name(), set.Name())
			if err != nil {
				// Discovery grants no credit without a valid receipt. The
				// caller separately verifies every explicitly selected set.
				continue
			}
			relevant := false
			for _, file := range recorded.Files {
				if wanted[file.SHA256] && strings.HasPrefix(file.Path, "model/") {
					relevant = true
					break
				}
			}
			if !relevant {
				continue
			}
			if err := verifyReceiptShape(path, recorded); err != nil {
				return nil, fmt.Errorf("inspect reusable model set %s: %w", path, err)
			}
			for _, file := range recorded.Files {
				if wanted[file.SHA256] && strings.HasPrefix(file.Path, "model/") {
					found[file.SHA256] = ModelFile{Path: filepath.Join(path, filepath.FromSlash(file.Path)), Size: file.Size}
					delete(wanted, file.SHA256)
				}
			}
			if len(wanted) == 0 {
				return found, nil
			}
		}
	}
	return found, nil
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
