package catalogcmd

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
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	installverb "github.com/temper-sh/temper/internal/software/install"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

type releaseReader map[string][]byte

func (r releaseReader) Open(ctx context.Context, locator string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := r[locator]
	if !ok {
		return nil, fmt.Errorf("unexpected network read %q", locator)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestLatestCompileUsesResolvedSoftwareAndDryRunWritesNothing(t *testing.T) {
	reader := releaseReader{}
	for _, item := range []struct{ repo, tag, asset, root string }{
		{"ggml-org/llama.cpp", "b12000", "llama-b12000-bin-macos-arm64.tar.gz", "llama-b12000"},
		{"mostlygeek/llama-swap", "v300", "llama-swap_300_darwin_arm64.tar.gz", "."},
	} {
		var data bytes.Buffer
		zip := gzip.NewWriter(&data)
		archive := tar.NewWriter(zip)
		if err := archive.WriteHeader(&tar.Header{Name: filepath.ToSlash(filepath.Join(item.root, "server")), Mode: 0o755, Size: 4}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte("test")); err != nil {
			t.Fatal(err)
		}
		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}
		if err := zip.Close(); err != nil {
			t.Fatal(err)
		}
		assetURL := "https://github.com/" + item.repo + "/releases/download/" + item.tag + "/" + item.asset
		reader[assetURL] = data.Bytes()
		reader["https://api.github.com/repos/"+item.repo+"/releases/latest"], _ = json.Marshal(map[string]any{
			"tag_name": item.tag, "assets": []any{map[string]any{"name": item.asset, "browser_download_url": assetURL, "size": data.Len(), "digest": fmt.Sprintf("sha256:%x", sha256.Sum256(data.Bytes()))}},
		})
		reader["https://api.github.com/repos/"+item.repo+"/git/ref/tags/"+item.tag] = []byte(`{"object":{"type":"commit","sha":"` + strings.Repeat("a", 40) + `"}}`)
	}
	root := t.TempDir()
	out := filepath.Join(root, "execution.lock.json")
	args := []string{"--catalog", "../../catalog/qwen38-m5-refresh.json", "--selection", "../../catalog/qwen38-m5-refresh.selection.json", "--target", "darwin/arm64", "--software", "latest", "--out", out}
	var stdout, stderr bytes.Buffer
	if code := compile(context.Background(), append(append([]string{}, args...), "--dry-run"), &stdout, &stderr, reader); code != 0 {
		t.Fatalf("dry compile: %s", stderr.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("dry latest wrote files: %v, %v", entries, err)
	}
	if code := compile(context.Background(), args, &stdout, &stderr, reader); code != 0 {
		t.Fatalf("compile: %s", stderr.String())
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := lock.Projections()
	if err != nil {
		t.Fatal(err)
	}
	if projection.Software.Units["upstream-release:llama-cpp"].Version != "b12000" || projection.Software.Units["upstream-release:llama-swap"].Version != "v300" {
		t.Fatal("latest option kept the authored versions")
	}
	// Exercise the real install/receipt path using the same in-memory archives.
	// No downloaded code is executed, and all effects stay inside t.TempDir.
	installer, err := upstreamrelease.NewInstallationAdapter(reader)
	if err != nil {
		t.Fatal(err)
	}
	family, err := adapter.NewInstallationFamily(installer)
	if err != nil {
		t.Fatal(err)
	}
	softwareBytes, err := softwarelock.Marshal(projection.Software)
	if err != nil {
		t.Fatal(err)
	}
	softwarePath := filepath.Join(root, "software.lock.yaml")
	if err := os.WriteFile(softwarePath, softwareBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	options := installverb.Options{LockPath: softwarePath, Root: filepath.Join(root, "installed"), Installation: "latest", InvocationID: "install-latest"}
	installed, err := installverb.Run(context.Background(), options, family)
	if err != nil || !installed.Changed || installed.Effects != 2 {
		t.Fatalf("resolved latest install: %+v, %v", installed, err)
	}
	options.InvocationID = "install-latest-again"
	again, err := installverb.Run(context.Background(), options, family)
	if err != nil || again.Changed || again.ReceiptSHA256 != installed.ReceiptSHA256 {
		t.Fatalf("latest replay changed installation: %+v, %v", again, err)
	}
	// A failed read must not quietly use the recorded releases or publish a lock.
	failedPath := filepath.Join(root, "failed.json")
	args[len(args)-1] = failedPath
	if code := compile(context.Background(), args, &stdout, &stderr, releaseReader{}); code == 0 {
		t.Fatal("missing latest silently fell back")
	}
	if _, err := os.Stat(failedPath); !os.IsNotExist(err) {
		t.Fatalf("failed resolution published a lock: %v", err)
	}
}
