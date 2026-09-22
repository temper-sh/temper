package catalogcmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

type publishedSource struct{ channel, data publication.SignedArtifact }

func (s publishedSource) Channel(context.Context, string) (publication.SignedArtifact, error) {
	return s.channel, nil
}
func (s publishedSource) CatalogJSON(context.Context, string) (publication.SignedArtifact, error) {
	return s.data, nil
}

// Exercise the public signed publication through update, explicit selection and
// the real compiler. The transport is local fixture data; the test is offline.
func publishedRoot(t *testing.T) (string, distribution.Snapshot) {
	t.Helper()
	trust, err := catalogtrust.Production()
	if err != nil {
		t.Fatal(err)
	}
	tree := "../../docs/catalog"
	published, err := distribution.VerifyTree(tree, trust)
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	snapshot := filepath.Join(tree, "snapshots", published.SHA256)
	source := publishedSource{
		channel: publication.SignedArtifact{Data: read(filepath.Join(tree, "channels/stable/channel.yaml")), Signature: read(filepath.Join(tree, "channels/stable/channel.signature.yaml"))},
		data:    publication.SignedArtifact{Data: read(filepath.Join(snapshot, "catalog.json")), Signature: read(filepath.Join(snapshot, "catalog.signature.yaml"))},
	}
	root := filepath.Join(t.TempDir(), "catalog root")
	var out, stderr bytes.Buffer
	if code := runDistribution(context.Background(), []string{"update", "--root", root}, &out, &stderr, trust, source); code != 0 {
		t.Fatalf("update: %s", &stderr)
	}
	return root, published
}

func TestPublishedSelectionCompilesOfflineAndBindsExactPublication(t *testing.T) {
	root, published := publishedRoot(t)
	profile := sortedKeys(published.Document.Profiles)[0]
	parent := filepath.Dir(root)
	selectionPath := filepath.Join(parent, "selection.json")
	lockPath := filepath.Join(parent, "execution.lock.json")
	run := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, &out, &stderr); code != 0 {
			t.Fatalf("%v: %s", args, &stderr)
		}
		return out.String()
	}
	if out := run("catalog", "inspect", "--root", root, "--profile", profile, "--json"); !strings.Contains(out, published.SHA256) || !strings.Contains(out, profile) {
		t.Fatalf("inspection omitted selection/identity: %s", out)
	}
	selectArgs := []string{"catalog", "select", "--root", root, "--profile", profile, "--out", selectionPath}
	run(append(append([]string{}, selectArgs...), "--dry-run")...)
	if _, err := os.Stat(selectionPath); !os.IsNotExist(err) {
		t.Fatal("dry select wrote a selection")
	}
	run(selectArgs...)
	before, err := os.ReadFile(selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if out := run(selectArgs...); !strings.Contains(out, "unchanged") {
		t.Fatalf("selection replay: %s", out)
	}
	args := []string{"catalog", "compile", "--root", root, "--selection", selectionPath, "--target", "darwin/arm64", "--out", lockPath}
	run(append(append([]string{}, args...), "--dry-run")...)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("dry compile wrote a lock")
	}
	run(args...)
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := catalog.ParseLock(data)
	if err != nil {
		t.Fatal(err)
	}
	if lock.SourceSnapshotSHA256 != published.SHA256 || lock.Selection.Profile != profile {
		t.Fatal("compiled lock lost the authenticated source or explicit profile")
	}
	if out := run(args...); !strings.Contains(out, "unchanged") {
		t.Fatalf("compile replay: %s", out)
	}
	after, err := os.ReadFile(selectionPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("compilation rewrote the user's selection")
	}
	// Different existing user content requires a new destination, even when it
	// is valid catalog data. No implicit adoption or replacement is permitted.
	if err := os.WriteFile(selectionPath, []byte("user-owned content"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := Run(context.Background(), selectArgs, &out, &stderr); code == 0 {
		t.Fatal("selection replaced user content")
	}
	after, _ = os.ReadFile(selectionPath)
	if string(after) != "user-owned content" {
		t.Fatal("selection refusal changed user content")
	}
	unchanged, _ := os.ReadFile(lockPath)
	if !bytes.Equal(data, unchanged) {
		t.Fatal("selection changed an existing lock")
	}
}

func TestCompileRequiresExactlyOneCatalogSource(t *testing.T) {
	for _, sources := range [][]string{nil, {"--root", "missing", "--catalog", "missing"}} {
		args := append([]string{"catalog", "compile", "--selection", "missing", "--target", "darwin/arm64", "--out", "missing"}, sources...)
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, &out, &stderr); code != 2 {
			t.Fatalf("ambiguous source: exit=%d %s", code, &stderr)
		}
	}
}
