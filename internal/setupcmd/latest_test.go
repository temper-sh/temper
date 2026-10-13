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
	c.Resolve = latestResolver(reader)
	if code, output, diagnostics := configureRun(c, "--root", root, "--preset", compactLayout, "--software", "latest", "--dry-run", "--json"); code != 0 || !strings.Contains(output, "b11132") || !strings.Contains(output, "v257") {
		t.Fatalf("latest preview: code=%d %s %s", code, output, diagnostics)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry preview wrote root: %v", err)
	}
}

func TestLatestFallsBackToStablePerItemAndSaysWhy(t *testing.T) {
	reader := previewReleaseFixture(t)
	reader["https://github.com/ggml-org/llama.cpp/releases/download/b11132/llama-b11132-bin-macos-arm64.tar.gz"] = []byte("corrupt archive")
	root := filepath.Join(t.TempDir(), "absent")
	c := fixtureCommand(t)
	c.Resolve = latestResolver(reader)
	stable := guidedCatalog(t).Engines[guidedCatalog(t).Presets[compactLayout].Engine].Supply.Release.Version
	code, output, diagnostics := configureRun(c, "--root", root, "--preset", compactLayout, "--software", "latest", "--dry-run")
	if code != 0 || !strings.Contains(diagnostics, "SOFTWARE llama-cpp stable "+stable+" (latest unavailable:") || !strings.Contains(diagnostics, "SOFTWARE llama-swap latest v257") {
		t.Fatalf("fallback was not visible: code=%d %s %s", code, output, diagnostics)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry preview wrote root: %v", err)
	}
}

// latestResolver resolves with one shared lookup cache, as a command run does.
func latestResolver(reader previewReleases) SoftwareResolver {
	latest := &catalog.LatestReleases{Reader: reader}
	return func(ctx context.Context, d catalog.Document, id string, choice string) (catalog.Document, []catalog.Resolution, error) {
		return catalog.ResolveSoftware(ctx, d, id, choice, latest, true)
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
