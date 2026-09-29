package setupcmd

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
)

type previewReleases map[string][]byte

func (r previewReleases) Open(ctx context.Context, locator string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := r[locator]
	if !ok {
		return nil, fmt.Errorf("unexpected release or model download %q", locator)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestLatestWizardPreviewResolvesActualReleaseShapesWithoutWriting(t *testing.T) {
	reader := previewReleaseFixture(t)
	root := filepath.Join(t.TempDir(), "absent")
	c := fixtureCommand(t)
	c.Resolve = func(ctx context.Context, d catalog.Document, s string, choice string) (catalog.Document, error) {
		return catalog.ResolveSoftware(ctx, d, s, choice, reader)
	}
	if code, output, diagnostics := configureRun(c, "--root", root, "--preset", compactLayout, "--software", "latest", "--dry-run", "--json"); code != 0 || !strings.Contains(output, "b11132") || !strings.Contains(output, "v257") {
		t.Fatalf("latest preview: code=%d %s %s", code, output, diagnostics)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry preview wrote root: %v", err)
	}
}

func TestLatestWizardRefusesChangedArchiveInsteadOfRecordedFallback(t *testing.T) {
	reader := previewReleaseFixture(t)
	reader["https://github.com/ggml-org/llama.cpp/releases/download/b11132/llama-b11132-bin-macos-arm64.tar.gz"] = []byte("corrupt archive")
	root := filepath.Join(t.TempDir(), "absent")
	c := fixtureCommand(t)
	c.Resolve = func(ctx context.Context, d catalog.Document, s string, choice string) (catalog.Document, error) {
		return catalog.ResolveSoftware(ctx, d, s, choice, reader)
	}
	if code, _, diagnostics := configureRun(c, "--root", root, "--preset", compactLayout, "--software", "latest", "--dry-run"); code == 0 || !strings.Contains(diagnostics, "resolve llama-cpp") {
		t.Fatalf("changed latest silently fell back: code=%d diagnostic=%s", code, diagnostics)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("failed preview wrote root: %v", err)
	}
}

func previewReleaseFixture(t *testing.T) previewReleases {
	t.Helper()
	reader := previewReleases{}
	for _, source := range []struct{ repo, tag, asset, root string }{
		{"ggml-org/llama.cpp", "b11132", "llama-b11132-bin-macos-arm64.tar.gz", "llama-b11132"},
		{"mostlygeek/llama-swap", "v257", "llama-swap_257_darwin_arm64.tar.gz", "."},
	} {
		var archive bytes.Buffer
		compressed := gzip.NewWriter(&archive)
		tarball := tar.NewWriter(compressed)
		if err := tarball.WriteHeader(&tar.Header{Name: filepath.ToSlash(filepath.Join(source.root, "server")), Mode: 0o755, Size: 4}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarball.Write([]byte("test")); err != nil {
			t.Fatal(err)
		}
		if err := tarball.Close(); err != nil {
			t.Fatal(err)
		}
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
		assetURL := "https://github.com/" + source.repo + "/releases/download/" + source.tag + "/" + source.asset
		reader[assetURL] = archive.Bytes()
		metadata := map[string]any{"tag_name": source.tag, "assets": []any{map[string]any{
			"name": source.asset, "browser_download_url": assetURL, "size": archive.Len(),
			"digest": fmt.Sprintf("sha256:%x", sha256.Sum256(archive.Bytes())),
		}}}
		endpoint := "https://api.github.com/repos/" + source.repo
		if source.repo == "ggml-org/llama.cpp" {
			metadata["prerelease"] = true
			reader[endpoint+"/releases?per_page=20&page=1"], _ = json.Marshal([]any{
				map[string]any{"tag_name": "v0.4.1", "assets": []any{}},
				map[string]any{"tag_name": "b11133", "prerelease": true, "assets": []any{}},
				metadata,
			})
		} else {
			reader[endpoint+"/releases/latest"], _ = json.Marshal(metadata)
		}
		reader[endpoint+"/git/ref/tags/"+source.tag] = []byte(`{"object":{"type":"commit","sha":"` + strings.Repeat("a", 40) + `"}}`)
	}
	return reader
}
