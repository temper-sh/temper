package hfcache

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSharedWeightRemovalPreservesLiteralFilenames(t *testing.T) {
	for _, name := range []string{"weights/model name.gguf", "weights/model%20name.gguf", "weights/日本語.gguf", "weights/model#1?.gguf"} {
		t.Run(name, func(t *testing.T) {
			entry := downloadEntry()
			entry.Name = name
			_, args, err := removeCommand("/cache", entry, func(string) (string, error) { return "/bin/hf", nil })
			if err != nil || args[2] != "hf://models/"+entry.Repo+"/"+name {
				t.Fatal(args, err)
			}
		})
	}
}

func TestHFFileDeletionCapabilityUsesReadOnlyHelp(t *testing.T) {
	for _, supported := range []bool{true, false} {
		name := "unsupported"
		if supported {
			name = "supported"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			help := "Remove cached repositories or revisions."
			if supported {
				help = "hf:// URIs (repo or file)"
			}
			script := "#!/bin/sh\n[ \"$*\" = 'cache rm --help' ] || exit 9\nprintf '%s\\n' '" + help + "'\n"
			if err := os.WriteFile(filepath.Join(dir, "hf"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			err := CheckFileRemoval(context.Background())
			if supported && err != nil || !supported && (err == nil || !strings.Contains(err.Error(), "brew upgrade hf")) {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := CheckFileRemoval(ctx); !errors.Is(err, context.Canceled) {
				t.Fatal("lost cancellation", err)
			}
		})
	}
}

func TestRemovalProcessPassesOneLiteralFileArgument(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEMPER_TEST_ARGS\"\n"
	if err := os.WriteFile(filepath.Join(dir, "hf"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TEMPER_TEST_ARGS", argsFile)
	entry := downloadEntry()
	entry.Name = "weights/model space%20.gguf"
	if err := RemoveFile(context.Background(), dir, entry, io.Discard); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsFile)
	want := "cache\nrm\nhf://models/" + entry.Repo + "/" + entry.Name + "\n--cache-dir\n" + dir + "\n--yes\n"
	if err != nil || string(raw) != want {
		t.Fatal(string(raw), err)
	}
}

func TestSharedWeightRemovalUsesAnExplicitFileAndCacheWithoutAShell(t *testing.T) {
	entry := Entry{Repo: "owner/model", Revision: strings.Repeat("a", 40), Name: "weights/model.gguf", SHA256: strings.Repeat("b", 64)}
	path, args, err := removeCommand("/cache with spaces", entry, func(s string) (string, error) {
		if s != "hf" {
			t.Fatal(s)
		}
		return "/bin/hf", nil
	})
	if err != nil || path != "/bin/hf" || !reflect.DeepEqual(args, []string{"cache", "rm", "hf://models/owner/model/weights/model.gguf", "--cache-dir", "/cache with spaces", "--yes"}) {
		t.Fatal(path, args, err)
	}
	for _, pair := range [][2]string{{"relative", "owner/model"}, {"/cache", "--all"}, {"/cache", "owner/model; rm -rf /"}, {"/cache", "owner/a/b"}} {
		bad := entry
		bad.Repo = pair[1]
		if _, _, err := removeCommand(pair[0], bad, func(string) (string, error) { t.Fatal("looked up a client for invalid deletion"); return "", nil }); err == nil {
			t.Fatal(pair)
		}
	}
	for _, name := range []string{"../escape", "/absolute", "."} {
		bad := entry
		bad.Name = name
		if _, _, err := removeCommand("/cache", bad, func(string) (string, error) { t.Fatal("invalid file reached client lookup"); return "", nil }); err == nil {
			t.Fatal(name)
		}
	}
}
