package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModulesFromListReturnsSortedUniqueThirdPartyModules(t *testing.T) {
	t.Parallel()
	input := strings.NewReader(`
{"Standard":true}
{"Module":{"Path":"github.com/temper-sh/temper","Main":true,"Dir":"/repo"}}
{"Module":{"Path":"example.com/z","Version":"v2.0.0","Dir":"/z"}}
{"Module":{"Path":"example.com/a","Version":"v1.0.0","Dir":"/a"}}
{"Module":{"Path":"example.com/z","Version":"v2.0.0","Dir":"/z"}}
`)
	modules, err := modulesFromList(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 2 {
		t.Fatalf("module count = %d, want 2", len(modules))
	}
	if got := modules[0]; got.Path != "example.com/a" || got.Version != "v1.0.0" || got.Dir != "/a" {
		t.Fatalf("module 0 = %#v", got)
	}
	if got := modules[1]; got.Path != "example.com/z" || got.Version != "v2.0.0" || got.Dir != "/z" {
		t.Fatalf("module 1 = %#v", got)
	}
}

func TestModulesFromListRejectsReplacementAndConflictingDirectory(t *testing.T) {
	t.Parallel()
	tests := []string{
		`{"Module":{"Path":"example.com/a","Version":"v1.0.0","Dir":"/a","Replace":{"Path":"example.com/b","Version":"v1.0.0","Dir":"/b"}}}`,
		"{\"Module\":{\"Path\":\"example.com/a\",\"Version\":\"v1.0.0\",\"Dir\":\"/a\"}}\n" +
			`{"Module":{"Path":"example.com/a","Version":"v1.0.0","Dir":"/other"}}`,
	}
	for _, input := range tests {
		if _, err := modulesFromList(strings.NewReader(input)); err == nil {
			t.Fatalf("modulesFromList(%q) succeeded", input)
		}
	}
}

func TestCommitFileIsSecondRunCleanAndRefusesCollision(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "artifact")
	unchanged, err := commitFile(path, []byte("one"), 0o644)
	if err != nil || unchanged {
		t.Fatalf("first commit = unchanged %t, err %v", unchanged, err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err = commitFile(path, []byte("one"), 0o644)
	if err != nil || !unchanged {
		t.Fatalf("second commit = unchanged %t, err %v", unchanged, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("second commit changed mtime: %s -> %s", before.ModTime(), after.ModTime())
	}
	if _, err := commitFile(path, []byte("two"), 0o644); err == nil {
		t.Fatal("different bytes succeeded")
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, []byte("one")) {
		t.Fatalf("artifact changed: %q, %v", got, err)
	}
}

func TestTargetEnvironmentPinsReleaseBuildSettings(t *testing.T) {
	t.Parallel()
	got := targetEnvironment([]string{
		"PATH=/usr/bin",
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=1",
		"GOFLAGS=-race",
		"GOWORK=/tmp/go.work",
		"GOENV=/tmp/go-env",
		"GOTOOLCHAIN=auto",
		"SOURCE_DATE_EPOCH=123",
	})
	want := []string{
		"PATH=/usr/bin",
		"GOOS=darwin",
		"GOARCH=arm64",
		"CGO_ENABLED=0",
		"GOFLAGS=",
		"GOWORK=off",
		"GOENV=off",
		"GOTOOLCHAIN=local",
		"SOURCE_DATE_EPOCH=0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("environment = %#v, want %#v", got, want)
	}
}

func TestValidateBinaryVersion(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	matching := filepath.Join(directory, "matching")
	wrong := filepath.Join(directory, "wrong")
	for path, body := range map[string]string{
		matching: "#!/bin/sh\nprintf 'temper 1.2.3\\n'\n",
		wrong:    "#!/bin/sh\nprintf 'temper 9.9.9\\n'\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateBinaryVersion(matching, "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if err := validateBinaryVersion(wrong, "1.2.3"); err == nil {
		t.Fatal("wrong binary version succeeded")
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run([]string{"unknown"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}
