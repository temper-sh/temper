package repopolicy_test

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var durableNamePatterns = []struct {
	label   string
	pattern *regexp.Regexp
}{
	{"plan-local contract coordinate", regexp.MustCompile(`\bC[0-9]+\b`)},
	{"plan-local decision coordinate", regexp.MustCompile(`\bD[0-9]+\b`)},
	{"numbered source paragraph", regexp.MustCompile(`(?:PLAN(?:\.md)?|SPEC(?:\.md)?|FINDINGS)\s*(?:§|#)\s*[0-9]+|§[0-9]+`)},
	{"plan-local milestone or phase coordinate", regexp.MustCompile(`\b(?:PLAN|milestone)\s+M[0-9]+\b|\bM[0-9]+\s+Phase\s+[A-Z]\b|\bPhase\s+[A-Z]\s+item\s+[0-9]+\b`)},
}

func TestDurableSurfacesUseSemanticNames(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository policy test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	extensions := map[string]bool{
		".go": true, ".json": true, ".md": true, ".py": true,
		".tsv": true, ".yaml": true, ".yml": true,
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == filepath.Join("docs", "PLAN.md") || !extensions[filepath.Ext(path)] {
			return nil
		}
		return inspectDurableNames(t, path, relative)
	})
	if err != nil {
		t.Fatalf("scan durable repository surfaces: %v", err)
	}
}

func inspectDurableNames(t *testing.T, path, relative string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		for _, check := range durableNamePatterns {
			if match := check.pattern.FindString(text); match != "" {
				t.Errorf("%s:%d: %s %q; use a semantic name or versioned identity", filepath.ToSlash(relative), line, check.label, strings.TrimSpace(match))
			}
		}
	}
	return scanner.Err()
}
