package catalogcmd

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"

	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
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
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"fixture": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	sign := func(data []byte) publication.SignedArtifact {
		envelope := fmt.Sprintf("schema: %s\nkey_id: fixture\nalgorithm: ed25519\nsignature: %s\n", publication.SignatureSchemaV1, base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)))
		return publication.SignedArtifact{Data: data, Signature: []byte(envelope)}
	}
	channel := []byte(fmt.Sprintf("schema: temper-catalog-channel/v1\nchannel: stable\ncatalog:\n  schema: temper-catalog/v3\n  sequence: 1\n  sha256: %s\n  locator: https://example.test/snapshots/%s/\n", digest, digest))
	source := publishedSource{channel: sign(channel), data: sign(data)}
	root := filepath.Join(t.TempDir(), "catalog root")
	var out, stderr bytes.Buffer
	if code := runDistribution(context.Background(), []string{"update", "--root", root}, &out, &stderr, trust, source); code != 0 {
		t.Fatalf("update: %s", &stderr)
	}
	published, err := distribution.Read(root, trust)
	if err != nil {
		t.Fatal(err)
	}
	return root, published
}

func TestPublishedPresetCompilesOfflineAndBindsExactPublication(t *testing.T) {
	root, published := publishedRoot(t)
	id := sortedKeys(published.Document.Presets)[0]
	lockPath := filepath.Join(filepath.Dir(root), "execution.lock.json")
	run := func(args ...string) string {
		t.Helper()
		var out, diagnostic bytes.Buffer
		if code := compile(context.Background(), args, &out, &diagnostic, nil, publishedTrust(t)); code != 0 {
			t.Fatalf("%v: %s", args, &diagnostic)
		}
		return out.String()
	}
	args := []string{"--root", root, "--preset", id, "--target", "darwin/arm64", "--out", lockPath}
	run(append(append([]string{}, args...), "--dry-run")...)
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatal("dry compile wrote a lock")
	}
	run(args...)
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := catalog.ParseLock(raw)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Preset != id || locked.SourceSnapshotSHA256 != published.SHA256 {
		t.Fatal("preset lost")
	}
	if out := run(args...); !strings.Contains(out, "unchanged") {
		t.Fatal(out)
	}
	if err := os.WriteFile(lockPath, []byte("user-owned content"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := compile(context.Background(), args, &out, &diag, nil, publishedTrust(t)); code == 0 {
		t.Fatal("replaced user content")
	}
	after, _ := os.ReadFile(lockPath)
	if string(after) != "user-owned content" {
		t.Fatal("failed compile changed output")
	}
}

func TestCompileRequiresExactlyOneCatalogSource(t *testing.T) {
	for _, sources := range [][]string{nil, {"--root", "missing", "--catalog", "missing"}} {
		args := append([]string{"catalog", "compile", "--preset", "missing", "--target", "darwin/arm64", "--out", "missing"}, sources...)
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), args, &out, &stderr); code != 2 {
			t.Fatalf("ambiguous source: exit=%d %s", code, &stderr)
		}
	}
}

func publishedTrust(t *testing.T) publication.TrustRoot {
	t.Helper()
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"fixture": key.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	return trust
}
