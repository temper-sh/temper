package catalogcmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/software"
	"gopkg.in/yaml.v3"
)

func descriptionCatalog(t *testing.T, format string) (string, []byte) {
	t.Helper()
	raw, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	if format == "yaml" {
		d, err := catalog.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		raw, err = yaml.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "catalog with spaces."+format)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

func describeRun(ctx context.Context, args ...string) (int, string, string) {
	var out, diagnostics bytes.Buffer
	code := Run(ctx, append([]string{"catalog", "describe"}, args...), &out, &diagnostics)
	return code, out.String(), diagnostics.String()
}

func TestDescriptionEditIsAtomicSecondRunCleanAndPreservesExecution(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			path, original := descriptionCatalog(t, format)
			before, err := catalog.Parse(original)
			if err != nil {
				t.Fatal(err)
			}
			const artifact = "qwen3.5-4b-q4km"
			const custom = "My everyday assistant — I check its dates and quotations."
			args := []string{"--catalog", path, "--artifact", artifact, "--description", custom, "--assessment-url", "https://example.test/my-notes"}
			if code, out, diag := describeRun(context.Background(), append(args, "--dry-run")...); code != 0 || !strings.Contains(out, "would-write") || !strings.Contains(out, custom) {
				t.Fatalf("dry edit: %d %s %s", code, out, diag)
			}
			raw, _ := os.ReadFile(path)
			entries, _ := os.ReadDir(filepath.Dir(path))
			if !bytes.Equal(raw, original) || len(entries) != 1 {
				t.Fatal("dry edit wrote a file or changed the catalog")
			}
			if code, out, diag := describeRun(context.Background(), args...); code != 0 || !strings.Contains(out, "written") {
				t.Fatalf("edit: %d %s %s", code, out, diag)
			}
			raw, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := catalog.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			if after.Artifacts[artifact].Description != custom {
				t.Fatal("custom description not stored")
			}
			info, _ := os.Stat(path)
			if info.Mode().Perm() != 0o600 {
				t.Fatal("edit changed file permissions")
			}
			for _, profile := range []string{"qwen3.5-4b-local", "qwen3.5-4b-utility"} {
				s := catalog.Selection{Schema: catalog.SelectionSchema, Profile: profile}
				a, err := catalog.Compile(before, s, software.Target{OS: "darwin", Arch: "arm64"})
				if err != nil {
					t.Fatal(err)
				}
				b, err := catalog.Compile(after, s, a.Target)
				if err != nil {
					t.Fatal(err)
				}
				if a.Digests.Profile != b.Digests.Profile || a.SourceSnapshotSHA256 == b.SourceSnapshotSHA256 {
					t.Fatal("description changed execution identity or not the source identity")
				}
				p, err := a.Projections()
				if err != nil {
					t.Fatal(err)
				}
				q, err := b.Projections()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(p, q) {
					t.Fatal("editorial change altered runtime projections")
				}
			}
			preserved := after.Artifacts[artifact]
			preserved.Description = before.Artifacts[artifact].Description
			preserved.AssessmentURL = before.Artifacts[artifact].AssessmentURL
			after.Artifacts[artifact] = preserved
			if !reflect.DeepEqual(after, before) {
				t.Fatal("edit changed unrelated catalog facts")
			}
			if code, out, diag := describeRun(context.Background(), args...); code != 0 || !strings.Contains(out, "unchanged") {
				t.Fatalf("replay: %d %s %s", code, out, diag)
			}
			again, _ := os.ReadFile(path)
			if !bytes.Equal(raw, again) {
				t.Fatal("unchanged edit rewrote bytes")
			}
			if code, out, diag := describeRun(context.Background(), "--catalog", path, "--artifact", artifact, "--description", "New automated suggestion", "--assessment-url", "https://example.test/new", "--if-empty"); code != 0 || !strings.Contains(out, "unchanged") {
				t.Fatalf("refresh: %d %s %s", code, out, diag)
			}
			again, _ = os.ReadFile(path)
			if !bytes.Equal(raw, again) {
				t.Fatal("suggestion replaced custom copy or its link")
			}
			entries, _ = os.ReadDir(filepath.Dir(path))
			if len(entries) != 1 {
				t.Fatal("edit left staging or lock files")
			}
		})
	}
}

func TestDescriptionFileInputAndExplicitClear(t *testing.T) {
	path, _ := descriptionCatalog(t, "json")
	text := filepath.Join(t.TempDir(), "assessment.txt")
	if err := os.WriteFile(text, []byte("Useful for my extraction work.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--catalog", path, "--artifact", "qwen3.5-4b-q4km"}
	if code, out, diag := describeRun(context.Background(), append(args, "--description-file", text)...); code != 0 || !strings.Contains(out, "Useful for my extraction work.") {
		t.Fatalf("file edit: %d %s %s", code, out, diag)
	}
	if code, _, diag := describeRun(context.Background(), append(args, "--description", "", "--assessment-url", "")...); code != 0 {
		t.Fatal(diag)
	}
	raw, _ := os.ReadFile(path)
	d, err := catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Artifacts["qwen3.5-4b-q4km"].Description != "" {
		t.Fatal("explicit clear failed")
	}
	if code, _, diag := describeRun(context.Background(), append(args, "--description", "First suggestion", "--if-empty")...); code != 0 {
		t.Fatal(diag)
	}
	raw, _ = os.ReadFile(path)
	d, err = catalog.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Artifacts["qwen3.5-4b-q4km"].Description != "First suggestion" {
		t.Fatal("empty description did not accept a suggestion")
	}
}

func TestDescriptionRefusalsLeaveCatalogUntouched(t *testing.T) {
	for _, name := range []string{"unknown artifact", "invalid URL", "control text", "cancelled", "symlink", "busy"} {
		t.Run(name, func(t *testing.T) {
			path, original := descriptionCatalog(t, "json")
			input := path
			args := []string{"--artifact", "qwen3.5-4b-q4km", "--description", "My assessment"}
			ctx := context.Background()
			switch name {
			case "unknown artifact":
				args[1] = "missing"
			case "invalid URL":
				args = append(args, "--assessment-url", "file:///private/notes")
			case "control text":
				args[3] = "hidden\x1b[31m"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "symlink":
				input = filepath.Join(filepath.Dir(path), "link.json")
				if err := os.Symlink(path, input); err != nil {
					t.Fatal(err)
				}
			case "busy":
				f, err := lockDescriptionParent(filepath.Dir(path))
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
			}
			args = append(args, "--catalog", input)
			if code, _, diag := describeRun(ctx, args...); code == 0 {
				t.Fatalf("refusal succeeded: %s", diag)
			}
			raw, _ := os.ReadFile(path)
			if !bytes.Equal(raw, original) {
				t.Fatal("failed edit changed the catalog")
			}
		})
	}
}

func TestDescriptionEditRefusesChangedFileBeforeCommit(t *testing.T) {
	path, original := descriptionCatalog(t, "json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	newer := append(append([]byte{}, original...), '\n')
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceDescription(context.Background(), path, original, []byte("stale update"), info); err == nil {
		t.Fatal("concurrent edit was overwritten")
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Equal(raw, newer) {
		t.Fatal("newer authoring edit was lost")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatal("failed edit left staging files")
	}
}
