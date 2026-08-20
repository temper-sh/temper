// Package artifactset defines and verifies one immutable layout artifact set.
package artifactset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
)

const receiptSchema = "temper-artifact-set/v1"

// ErrNotMaterialized means the immutable artifact-set directory is absent.
var ErrNotMaterialized = errors.New("artifact set is not materialized")

// ErrContentMismatch means a full-byte audit found data that does not match
// the SHA-256 selected by the lock.
var ErrContentMismatch = errors.New("artifact content does not match lock")

// File is one data file required by an artifact set.
type File struct {
	Path   string
	SHA256 string
}

// Record is the persistent receipt metadata for one verified data file.
type Record struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Set is the exact artifact identity selected by one manifest layout and lock
// entry. Its fields stay private so callers cannot weaken its invariants.
type Set struct {
	root     string
	layoutID string
	digest   string
	model    string
	files    []File
}

// Inspection contains trusted local facts exposed only after the immutable
// set passes receipt and shape admission.
type Inspection struct {
	ModelBytes int64
}

type receipt struct {
	Schema      string   `json:"schema"`
	Layout      string   `json:"layout"`
	EntryDigest string   `json:"entry_digest"`
	Files       []Record `json:"files"`
}

// New binds a validated manifest layout to its corresponding lock entry.
func New(root, layoutID string, layout manifest.Layout, entry lockfile.Entry, patches map[string]manifest.Patch) (Set, error) {
	if entry.Repo != layout.Model.Repo {
		return Set{}, fmt.Errorf("layout %q repo drift: manifest has %q, lock has %q", layoutID, layout.Model.Repo, entry.Repo)
	}
	if len(entry.Files) != 1 || entry.Files[0].Name != layout.Model.File {
		return Set{}, fmt.Errorf("layout %q selected model file drift: manifest has %q", layoutID, layout.Model.File)
	}

	model := filepath.ToSlash(filepath.Join("model", layout.Model.File))
	files := []File{{
		Path:   model,
		SHA256: entry.Files[0].SHA256,
	}}
	if layout.ChatTemplate == "" {
		if len(entry.Patches) != 0 {
			return Set{}, fmt.Errorf("layout %q selected patch drift: manifest selects no patch", layoutID)
		}
	} else {
		if len(entry.Patches) != 1 || entry.Patches[0].Name != layout.ChatTemplate {
			return Set{}, fmt.Errorf("layout %q selected patch drift: manifest has %q", layoutID, layout.ChatTemplate)
		}
		definition, ok := patches[layout.ChatTemplate]
		if !ok {
			return Set{}, fmt.Errorf("layout %q selects undefined patch %q", layoutID, layout.ChatTemplate)
		}
		files = append(files, File{
			Path:   filepath.ToSlash(filepath.Join("patches", layout.ChatTemplate, definition.File)),
			SHA256: entry.Patches[0].SHA256,
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Set{root: root, layoutID: layoutID, digest: entry.Digest(), model: model, files: files}, nil
}

// Path returns the immutable artifact-set directory.
func (s Set) Path() string {
	return filepath.Join(s.root, "artifacts", "layouts", s.layoutID, s.digest)
}

// Digest returns the lock entry's content-derived artifact-set identity.
func (s Set) Digest() string { return s.digest }

// Files returns a defensive copy of the set's expected data files.
func (s Set) Files() []File { return append([]File(nil), s.files...) }

// RootRelativeFiles returns the stable paths reported by fetch.
func (s Set) RootRelativeFiles() []string {
	base := filepath.ToSlash(filepath.Join("artifacts", "layouts", s.layoutID, s.digest))
	files := make([]string, 0, len(s.files)+1)
	for _, file := range s.files {
		files = append(files, base+"/"+file.Path)
	}
	files = append(files, base+"/receipt.json")
	sort.Strings(files)
	return files
}

// Receipt returns the canonical receipt for records produced while fetch
// hashes and stages the set's data files.
func (s Set) Receipt(records []Record) ([]byte, error) {
	validated, err := s.validateRecords(records)
	if err != nil {
		return nil, err
	}
	value := receipt{
		Schema:      receiptSchema,
		Layout:      s.layoutID,
		EntryDigest: s.digest,
		Files:       validated,
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode artifact receipt: %w", err)
	}
	return append(data, '\n'), nil
}

// Verify proves that the canonical receipt identifies this immutable set and
// that the local tree has exactly the recorded regular files and sizes. Fetch
// established the hashes before publishing the receipt; this routine avoids a
// multi-gigabyte rehash on every apply.
func (s Set) Verify() error {
	_, err := s.Inspect()
	return err
}

// Inspect performs routine receipt and shape verification and returns the
// recorded model size established when fetch hashed and published the set.
func (s Set) Inspect() (Inspection, error) {
	target := s.Path()
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return Inspection{}, fmt.Errorf("%w: %s", ErrNotMaterialized, target)
	}
	if err != nil {
		return Inspection{}, fmt.Errorf("inspect artifact set: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return Inspection{}, errors.New("artifact set is not a regular directory")
	}

	receiptPath := filepath.Join(target, "receipt.json")
	receiptInfo, err := os.Lstat(receiptPath)
	if errors.Is(err, fs.ErrNotExist) {
		return Inspection{}, errors.New("artifact set has no regular receipt.json")
	}
	if err != nil {
		return Inspection{}, fmt.Errorf("inspect artifact receipt: %w", err)
	}
	if !receiptInfo.Mode().IsRegular() || receiptInfo.Mode()&os.ModeSymlink != 0 {
		return Inspection{}, errors.New("artifact set has no regular receipt.json")
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		return Inspection{}, fmt.Errorf("read artifact receipt: %w", err)
	}
	var recorded receipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&recorded); err != nil {
		return Inspection{}, fmt.Errorf("decode artifact receipt: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Inspection{}, fmt.Errorf("decode artifact receipt: %w", err)
	}
	if recorded.Schema != receiptSchema || recorded.Layout != s.layoutID || recorded.EntryDigest != s.digest {
		return Inspection{}, errors.New("artifact receipt does not identify the requested immutable set")
	}
	canonical, err := s.Receipt(recorded.Files)
	if err != nil {
		return Inspection{}, fmt.Errorf("artifact receipt: %w", err)
	}
	if !bytes.Equal(data, canonical) {
		return Inspection{}, errors.New("artifact receipt is not in canonical form")
	}
	sizes := make(map[string]int64, len(recorded.Files))
	for _, file := range recorded.Files {
		sizes[file.Path] = file.Size
	}
	if err := verifyShape(target, sizes); err != nil {
		return Inspection{}, fmt.Errorf("artifact set is malformed: %w", err)
	}
	return Inspection{ModelBytes: sizes[s.model]}, nil
}

// VerifyContent performs routine receipt and shape verification, then streams
// every data file and compares its SHA-256 directly with the lock selection.
func (s Set) VerifyContent(ctx context.Context) error {
	_, err := s.InspectContent(ctx)
	return err
}

// InspectContent performs routine inspection, then streams every data file
// and compares its SHA-256 directly with the lock selection.
func (s Set) InspectContent(ctx context.Context) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	inspection, err := s.Inspect()
	if err != nil {
		return Inspection{}, err
	}
	for _, expected := range s.files {
		if err := ctx.Err(); err != nil {
			return Inspection{}, err
		}
		actual, err := fileSHA256(ctx, filepath.Join(s.Path(), filepath.FromSlash(expected.Path)))
		if err != nil {
			return Inspection{}, fmt.Errorf("hash artifact file %q: %w", expected.Path, err)
		}
		if actual != expected.SHA256 {
			return Inspection{}, fmt.Errorf("%w: %q has %s, lock requires %s", ErrContentMismatch, expected.Path, actual, expected.SHA256)
		}
	}
	return inspection, nil
}

func (s Set) validateRecords(records []Record) ([]Record, error) {
	if len(records) != len(s.files) {
		return nil, errors.New("receipt has an unexpected file set")
	}
	expected := make(map[string]string, len(s.files))
	for _, file := range s.files {
		expected[file.Path] = file.SHA256
	}
	validated := append([]Record(nil), records...)
	seen := make(map[string]bool, len(records))
	for _, record := range validated {
		hash, ok := expected[record.Path]
		if !ok || record.SHA256 != hash || record.Size < 0 {
			return nil, fmt.Errorf("receipt has invalid metadata for %q", record.Path)
		}
		if seen[record.Path] {
			return nil, fmt.Errorf("receipt repeats %q", record.Path)
		}
		seen[record.Path] = true
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].Path < validated[j].Path })
	return validated, nil
}

func verifyShape(target string, sizes map[string]int64) error {
	expectedDirectories := map[string]bool{".": true}
	for name := range sizes {
		for directory := filepath.Dir(filepath.FromSlash(name)); directory != "."; directory = filepath.Dir(directory) {
			expectedDirectories[directory] = true
		}
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(target, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(target, current)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%q is a symlink", filepath.ToSlash(relative))
		}
		if entry.IsDir() {
			if !expectedDirectories[relative] {
				return fmt.Errorf("unexpected directory %q", filepath.ToSlash(relative))
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("%q is not a regular file", filepath.ToSlash(relative))
		}
		name := filepath.ToSlash(relative)
		if name == "receipt.json" {
			seen[name] = true
			return nil
		}
		size, ok := sizes[name]
		if !ok {
			return fmt.Errorf("unexpected file %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() != size {
			return fmt.Errorf("%q size is %d, receipt records %d", name, info.Size(), size)
		}
		seen[name] = true
		return nil
	})
	if err != nil {
		return err
	}
	if !seen["receipt.json"] {
		return errors.New("receipt.json is absent")
	}
	for name := range sizes {
		if !seen[name] {
			return fmt.Errorf("receipt file %q is absent", name)
		}
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func fileSHA256(ctx context.Context, path string) (returnHash string, returnErr error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()

	hash := sha256.New()
	buffer := make([]byte, 128<<10)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hash.Write(buffer[:count])
		}
		if errors.Is(readErr, io.EOF) {
			return hex.EncodeToString(hash.Sum(nil)), nil
		}
		if readErr != nil {
			return "", readErr
		}
	}
}
