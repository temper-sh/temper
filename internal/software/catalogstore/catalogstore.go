// Package catalogstore owns immutable software-catalog snapshots and the one
// concurrency-safe active-pointer commit.
package catalogstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software/catalog"
)

const (
	catalogFilename   = "catalog.yaml"
	signatureFilename = "catalog.signature.yaml"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Publication struct {
	CatalogData   []byte
	SignatureData []byte
	Digest        string
}

// Snapshot is one observed active-pointer state. Commit succeeds only while
// that pointer still contains exactly the same bytes or absence.
type Snapshot struct {
	Catalog       catalog.Snapshot
	CatalogData   []byte
	SignatureData []byte

	root         string
	activePath   string
	activeData   []byte
	activeExists bool
}

func Read(root string) (Snapshot, error) {
	resolved, err := datadir.Resolve(root)
	if err != nil {
		return Snapshot{}, err
	}
	softwareRoot := filepath.Join(resolved, "software")
	catalogRoot := filepath.Join(softwareRoot, "catalog")
	snapshotsRoot := filepath.Join(catalogRoot, "snapshots")
	for _, directory := range []string{resolved, softwareRoot, catalogRoot, snapshotsRoot} {
		if err := validateDirectoryIfExists(directory); err != nil {
			return Snapshot{}, fmt.Errorf("inspect software catalog store: %w", err)
		}
	}
	activePath := filepath.Join(catalogRoot, "active")
	data, exists, err := readActive(activePath)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{root: resolved, activePath: activePath, activeData: data, activeExists: exists}
	if !exists {
		return snapshot, nil
	}
	digest := string(data[:64])
	stored, err := readStoredSnapshot(filepath.Join(snapshotsRoot, digest), digest)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read active software catalog: %w", err)
	}
	snapshot.Catalog = stored.Catalog
	snapshot.CatalogData = append([]byte(nil), stored.CatalogData...)
	snapshot.SignatureData = append([]byte(nil), stored.SignatureData...)
	return snapshot, nil
}

func (s Snapshot) Exists() bool { return s.activeExists }

// Commit validates the complete immutable publication before filesystem
// effects, stores it by digest, then conditionally replaces the active pointer.
func (s Snapshot) Commit(ctx context.Context, candidate Publication) error {
	if s.root == "" || s.activePath == "" {
		return errors.New("software catalog snapshot has no store root")
	}
	if !digestPattern.MatchString(candidate.Digest) {
		return errors.New("software catalog publication digest must be 64 lowercase hexadecimal characters")
	}
	parsed, err := catalog.ParseSnapshot(candidate.CatalogData)
	if err != nil {
		return fmt.Errorf("validate software catalog before staging: %w", err)
	}
	if parsed.SHA256 != candidate.Digest {
		return fmt.Errorf("software catalog publication digest is %q, exact bytes digest to %q", candidate.Digest, parsed.SHA256)
	}
	if len(candidate.SignatureData) == 0 {
		return errors.New("software catalog publication signature is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.activeExists && bytes.Equal(s.activeData, []byte(candidate.Digest+"\n")) {
		return nil
	}

	catalogRoot := filepath.Dir(s.activePath)
	snapshotsRoot := filepath.Join(catalogRoot, "snapshots")
	created := make([]string, 0, 4)
	for _, directory := range []string{s.root, filepath.Join(s.root, "software"), catalogRoot, snapshotsRoot} {
		wasCreated, err := ensureDirectory(directory)
		if err != nil {
			removeEmptyDirectories(created)
			return fmt.Errorf("prepare software catalog store: %w", err)
		}
		if wasCreated {
			created = append(created, directory)
		}
	}

	publication := storedPublication{
		Catalog:       parsed,
		CatalogData:   append([]byte(nil), candidate.CatalogData...),
		SignatureData: append([]byte(nil), candidate.SignatureData...),
	}
	finalPath := filepath.Join(snapshotsRoot, candidate.Digest)
	if err := storeImmutableSnapshot(snapshotsRoot, finalPath, candidate.Digest, publication); err != nil {
		removeEmptyDirectories(created)
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	pointer, err := os.CreateTemp(catalogRoot, ".temper-catalog-active-*")
	if err != nil {
		return fmt.Errorf("stage active software catalog pointer: %w", err)
	}
	pointerPath := pointer.Name()
	defer func() { _ = os.Remove(pointerPath) }()
	if err := pointer.Chmod(0o644); err != nil {
		pointer.Close()
		return fmt.Errorf("set active software catalog pointer mode: %w", err)
	}
	if _, err := pointer.WriteString(candidate.Digest + "\n"); err != nil {
		pointer.Close()
		return fmt.Errorf("write active software catalog pointer: %w", err)
	}
	if err := pointer.Sync(); err != nil {
		pointer.Close()
		return fmt.Errorf("sync active software catalog pointer: %w", err)
	}
	if err := pointer.Close(); err != nil {
		return fmt.Errorf("close active software catalog pointer: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	currentData, currentExists, err := readActive(s.activePath)
	if err != nil {
		return fmt.Errorf("verify active software catalog before commit: %w", err)
	}
	if currentExists != s.activeExists || !bytes.Equal(currentData, s.activeData) {
		return errors.New("active software catalog changed concurrently; rerun command")
	}
	if err := os.Rename(pointerPath, s.activePath); err != nil {
		return fmt.Errorf("commit active software catalog pointer: %w", err)
	}
	if err := syncDirectory(catalogRoot); err != nil {
		return fmt.Errorf("sync software catalog directory: %w", err)
	}
	return nil
}

type storedPublication struct {
	Catalog       catalog.Snapshot
	CatalogData   []byte
	SignatureData []byte
}

func readActive(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("inspect active software catalog pointer: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, false, errors.New("active software catalog pointer must be a regular file, not a directory or symlink")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read active software catalog pointer: %w", err)
	}
	if len(data) != 65 || data[64] != '\n' || !digestPattern.Match(data[:64]) {
		return nil, false, errors.New("active software catalog pointer must contain exactly one lowercase SHA-256 digest and newline")
	}
	return data, true, nil
}

func readStoredSnapshot(path, digest string) (storedPublication, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return storedPublication{}, fmt.Errorf("inspect snapshot %q: %w", digest, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return storedPublication{}, fmt.Errorf("snapshot %q must be a directory, not a file or symlink", digest)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return storedPublication{}, fmt.Errorf("list snapshot %q: %w", digest, err)
	}
	if len(entries) != 2 || entries[0].Name() != signatureFilename || entries[1].Name() != catalogFilename {
		return storedPublication{}, fmt.Errorf("snapshot %q must contain exactly %s and %s", digest, catalogFilename, signatureFilename)
	}
	catalogData, err := readRegularFile(filepath.Join(path, catalogFilename))
	if err != nil {
		return storedPublication{}, fmt.Errorf("read snapshot %q catalog: %w", digest, err)
	}
	signatureData, err := readRegularFile(filepath.Join(path, signatureFilename))
	if err != nil {
		return storedPublication{}, fmt.Errorf("read snapshot %q signature: %w", digest, err)
	}
	if len(signatureData) == 0 {
		return storedPublication{}, fmt.Errorf("snapshot %q signature is empty", digest)
	}
	parsed, err := catalog.ParseSnapshot(catalogData)
	if err != nil {
		return storedPublication{}, err
	}
	if parsed.SHA256 != digest {
		return storedPublication{}, fmt.Errorf("snapshot directory digest is %q, catalog bytes digest to %q", digest, parsed.SHA256)
	}
	return storedPublication{Catalog: parsed, CatalogData: catalogData, SignatureData: signatureData}, nil
}

func readRegularFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file, not a directory or symlink")
	}
	return os.ReadFile(path)
}

func ensureDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("%s must be a directory, not a file or symlink", path)
		}
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return ensureDirectory(path)
		}
		return false, err
	}
	return true, nil
}

func validateDirectoryIfExists(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must be a directory, not a file or symlink", path)
	}
	return nil
}

func storeImmutableSnapshot(root, finalPath, digest string, publication storedPublication) error {
	if _, err := os.Lstat(finalPath); err == nil {
		return compareStoredSnapshot(finalPath, digest, publication)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect immutable software catalog snapshot: %w", err)
	}

	stagePath, err := os.MkdirTemp(root, ".temper-catalog-snapshot-*")
	if err != nil {
		return fmt.Errorf("stage immutable software catalog snapshot: %w", err)
	}
	defer removeSnapshotStage(stagePath)
	if err := writeExclusiveFile(filepath.Join(stagePath, catalogFilename), publication.CatalogData); err != nil {
		return fmt.Errorf("stage software catalog: %w", err)
	}
	if err := writeExclusiveFile(filepath.Join(stagePath, signatureFilename), publication.SignatureData); err != nil {
		return fmt.Errorf("stage software catalog signature: %w", err)
	}
	if err := syncDirectory(stagePath); err != nil {
		return fmt.Errorf("sync staged software catalog snapshot: %w", err)
	}
	if err := os.Rename(stagePath, finalPath); err != nil {
		if compareErr := compareStoredSnapshot(finalPath, digest, publication); compareErr == nil {
			return nil
		}
		return fmt.Errorf("commit immutable software catalog snapshot: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("sync software catalog snapshots directory: %w", err)
	}
	return nil
}

func compareStoredSnapshot(path, digest string, expected storedPublication) error {
	actual, err := readStoredSnapshot(path, digest)
	if err != nil {
		return fmt.Errorf("existing immutable software catalog snapshot is invalid: %w", err)
	}
	if !bytes.Equal(actual.CatalogData, expected.CatalogData) || !bytes.Equal(actual.SignatureData, expected.SignatureData) {
		return errors.New("existing immutable software catalog snapshot differs from publication")
	}
	return nil
}

func writeExclusiveFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func removeSnapshotStage(path string) {
	_ = os.Remove(filepath.Join(path, catalogFilename))
	_ = os.Remove(filepath.Join(path, signatureFilename))
	_ = os.Remove(path)
}

func removeEmptyDirectories(paths []string) {
	for index := len(paths) - 1; index >= 0; index-- {
		_ = os.Remove(paths[index])
	}
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
