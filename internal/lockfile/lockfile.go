// Package lockfile parses and validates Temper's machine resolution lock.
package lockfile

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-lock/v1"

var (
	idPattern       = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Document struct {
	Schema  string           `yaml:"schema"`
	Entries map[string]Entry `yaml:"entries"`
}

type Entry struct {
	Repo     string  `yaml:"repo"`
	Revision string  `yaml:"revision"`
	Files    []File  `yaml:"files"`
	Patches  []Patch `yaml:"patches,omitempty"`
	Resolved string  `yaml:"resolved"`
}

type File struct {
	Name   string `yaml:"name"`
	SHA256 string `yaml:"sha256"`
}

type Patch struct {
	Name   string `yaml:"name"`
	SHA256 string `yaml:"sha256"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "lock invalid: " + strings.Join(e.Problems, "; ")
}

func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode lock: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode lock: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode lock: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if len(d.Entries) == 0 {
		problem("entries must not be empty")
	}

	for _, id := range sortedKeys(d.Entries) {
		entry := d.Entries[id]
		if !idPattern.MatchString(id) {
			problem("entry id %q is not a lowercase stable id", id)
		}
		if !repoPattern.MatchString(entry.Repo) {
			problem("entry %q repo %q must be owner/name", id, entry.Repo)
		}
		if !revisionPattern.MatchString(entry.Revision) {
			problem("entry %q revision must be a 40-character lowercase commit hash", id)
		}
		if len(entry.Files) == 0 {
			problem("entry %q files must not be empty", id)
		}
		seenFiles := map[string]bool{}
		for index, file := range entry.Files {
			location := fmt.Sprintf("entry %q files[%d]", id, index)
			if !safeRelativePath(file.Name) {
				problem("%s name %q is not a safe relative path", location, file.Name)
			}
			if seenFiles[file.Name] {
				problem("entry %q repeats file %q", id, file.Name)
			}
			seenFiles[file.Name] = true
			if !sha256Pattern.MatchString(file.SHA256) {
				problem("%s sha256 must be 64 lowercase hexadecimal characters", location)
			}
		}
		seenPatches := map[string]bool{}
		for index, patch := range entry.Patches {
			location := fmt.Sprintf("entry %q patches[%d]", id, index)
			if !idPattern.MatchString(patch.Name) {
				problem("%s name %q is not a lowercase stable id", location, patch.Name)
			}
			if seenPatches[patch.Name] {
				problem("entry %q repeats patch %q", id, patch.Name)
			}
			seenPatches[patch.Name] = true
			if !sha256Pattern.MatchString(patch.SHA256) {
				problem("%s sha256 must be 64 lowercase hexadecimal characters", location)
			}
		}
		if _, err := time.Parse("2006-01-02", entry.Resolved); err != nil {
			problem("entry %q resolved %q must be YYYY-MM-DD", id, entry.Resolved)
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func (d Document) Entry(id string) (Entry, bool) {
	entry, ok := d.Entries[id]
	return entry, ok
}

func Empty() Document {
	return Document{Schema: SchemaV1, Entries: map[string]Entry{}}
}

func (d Document) WithMissing(additions map[string]Entry) (Document, error) {
	entries := make(map[string]Entry, len(d.Entries)+len(additions))
	for id, entry := range d.Entries {
		entries[id] = entry
	}
	for id, entry := range additions {
		if _, exists := entries[id]; exists {
			return Document{}, fmt.Errorf("lock entry %q already exists", id)
		}
		entries[id] = entry
	}
	candidate := Document{Schema: d.Schema, Entries: entries}
	if err := candidate.Validate(); err != nil {
		return Document{}, err
	}
	return candidate, nil
}

func (d Document) WithReplacements(replacements map[string]Entry) (Document, error) {
	entries := make(map[string]Entry, len(d.Entries))
	for id, entry := range d.Entries {
		entries[id] = entry
	}
	for id, entry := range replacements {
		if _, exists := entries[id]; !exists {
			return Document{}, fmt.Errorf("lock entry %q does not exist", id)
		}
		entries[id] = entry
	}
	candidate := Document{Schema: d.Schema, Entries: entries}
	if err := candidate.Validate(); err != nil {
		return Document{}, err
	}
	return candidate, nil
}

func Marshal(document Document) ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode lock: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close lock encoder: %w", err)
	}
	return output.Bytes(), nil
}

func (e Entry) Digest() string {
	hash := sha256.New()
	writeHashPart(hash, "temper-layout-entry/v1")
	writeHashPart(hash, "repo")
	writeHashPart(hash, e.Repo)
	writeHashPart(hash, "revision")
	writeHashPart(hash, e.Revision)
	files := append([]File(nil), e.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	writeHashPart(hash, "files")
	writeHashCount(hash, len(files))
	for _, file := range files {
		writeHashPart(hash, file.Name)
		writeHashPart(hash, file.SHA256)
	}
	patches := append([]Patch(nil), e.Patches...)
	sort.Slice(patches, func(i, j int) bool { return patches[i].Name < patches[j].Name })
	writeHashPart(hash, "patches")
	writeHashCount(hash, len(patches))
	for _, patch := range patches {
		writeHashPart(hash, patch.Name)
		writeHashPart(hash, patch.SHA256)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func writeHashCount(hash interface{ Write([]byte) (int, error) }, count int) {
	var value [8]byte
	binary.BigEndian.PutUint64(value[:], uint64(count))
	_, _ = hash.Write(value[:])
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) || strings.ContainsAny(path, "\r\n\x00") {
		return false
	}
	cleaned := filepath.Clean(path)
	return cleaned == path && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeHashPart(hash interface{ Write([]byte) (int, error) }, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = hash.Write(size[:])
	_, _ = hash.Write([]byte(value))
}
