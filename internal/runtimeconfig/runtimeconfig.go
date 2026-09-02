// Package runtimeconfig defines the canonical executable requirements emitted
// with one render generation and consumed by the receipt-bound probe.
package runtimeconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const SchemaV1 = "temper-render-runtime/v1"

var packagePattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)

type Requirement struct {
	Package            string `json:"package"`
	RelativeExecutable string `json:"relative_executable"`
}

type Document struct {
	Schema       string        `json:"schema"`
	Requirements []Requirement `json:"requirements"`
}

func Marshal(document Document) ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode runtime requirements: %w", err)
	}
	return append(data, '\n'), nil
}

func Parse(data []byte) (Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode runtime requirements: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode runtime requirements: multiple JSON values are not allowed")
		}
		return Document{}, fmt.Errorf("decode runtime requirements: %w", err)
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	canonical, err := Marshal(document)
	if err != nil {
		return Document{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Document{}, errors.New("runtime requirements are not in canonical form")
	}
	return document, nil
}

func (d Document) Validate() error {
	if d.Schema != SchemaV1 {
		return fmt.Errorf("runtime requirements schema is %q, want %q", d.Schema, SchemaV1)
	}
	if len(d.Requirements) == 0 {
		return errors.New("runtime requirements must not be empty")
	}
	seen := map[string]bool{}
	hasRouter := false
	for index, requirement := range d.Requirements {
		if !packagePattern.MatchString(requirement.Package) {
			return fmt.Errorf("runtime requirement %d package %q is not a lowercase stable id", index, requirement.Package)
		}
		if !safeRelativePath(requirement.RelativeExecutable) {
			return fmt.Errorf("runtime requirement %d executable %q is not a safe relative path", index, requirement.RelativeExecutable)
		}
		key := requirement.Package + "\x00" + requirement.RelativeExecutable
		if seen[key] {
			return fmt.Errorf("runtime requirements repeat %q", requirement.Package)
		}
		seen[key] = true
		if requirement.Package == "llama-swap" && requirement.RelativeExecutable == "llama-swap" {
			hasRouter = true
		}
	}
	if !sort.SliceIsSorted(d.Requirements, func(i, j int) bool {
		if d.Requirements[i].Package != d.Requirements[j].Package {
			return d.Requirements[i].Package < d.Requirements[j].Package
		}
		return d.Requirements[i].RelativeExecutable < d.Requirements[j].RelativeExecutable
	}) {
		return errors.New("runtime requirements must be sorted")
	}
	if !hasRouter {
		return errors.New("runtime requirements must include the exact llama-swap router")
	}
	return nil
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.Contains(path, `\`) || strings.ContainsAny(path, "\r\n\x00") {
		return false
	}
	clean := filepath.Clean(path)
	return clean == path && clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
