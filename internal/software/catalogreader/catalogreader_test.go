package catalogreader_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogreader"
	"github.com/temper-sh/temper/internal/software/catalogstore"
)

type capabilities struct {
	err      error
	sequence uint64
}

func (c *capabilities) ValidateCatalog(document catalog.Document) error {
	c.sequence = document.Sequence
	return c.err
}

func TestReadUsesAuthenticatedBootstrapWithoutCreatingTheDataRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent-root")
	trust, privateKey := trustRoot(t)
	data := catalogData(3)
	validator := &capabilities{}

	result, err := catalogreader.Read(root, trust, catalogreader.Bootstrap{
		CatalogData: data, SignatureData: sign(data, privateKey),
	}, validator)
	if err != nil {
		t.Fatal(err)
	}
	if result.Origin != catalogreader.OriginBootstrap || result.Catalog.Document.Sequence != 3 || result.KeyID != "temper-catalog-test" {
		t.Fatalf("result = %#v", result)
	}
	if validator.sequence != 3 {
		t.Fatalf("validated sequence = %d", validator.sequence)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bootstrap read created data root: %v", err)
	}
}

func TestReadPrefersAndVerifiesTheActiveSnapshot(t *testing.T) {
	root := t.TempDir()
	trust, privateKey := trustRoot(t)
	activeData := catalogData(4)
	activeSignature := sign(activeData, privateKey)
	store, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(t.Context(), catalogstore.Publication{
		CatalogData: activeData, SignatureData: activeSignature, Digest: catalog.SnapshotDigest(activeData),
	}); err != nil {
		t.Fatal(err)
	}
	bootstrapData := catalogData(3)

	result, err := catalogreader.Read(root, trust, catalogreader.Bootstrap{
		CatalogData: bootstrapData, SignatureData: sign(bootstrapData, privateKey),
	}, &capabilities{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Origin != catalogreader.OriginActive || result.Catalog.Document.Sequence != 4 {
		t.Fatalf("result = %#v", result)
	}
}

func TestReadNeverFallsBackWhenTheActiveSnapshotWasTamperedWith(t *testing.T) {
	root := t.TempDir()
	trust, privateKey := trustRoot(t)
	activeData := catalogData(4)
	digest := catalog.SnapshotDigest(activeData)
	store, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Commit(t.Context(), catalogstore.Publication{
		CatalogData: activeData, SignatureData: sign(activeData, privateKey), Digest: digest,
	}); err != nil {
		t.Fatal(err)
	}
	signaturePath := filepath.Join(root, "software", "catalog", "snapshots", digest, "catalog.signature.yaml")
	if err := os.WriteFile(signaturePath, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bootstrapData := catalogData(3)

	_, err = catalogreader.Read(root, trust, catalogreader.Bootstrap{
		CatalogData: bootstrapData, SignatureData: sign(bootstrapData, privateKey),
	}, &capabilities{})
	if err == nil || !strings.Contains(err.Error(), "verify active software catalog signature") {
		t.Fatalf("error = %v", err)
	}
}

func TestReadRefusesCatalogUnsupportedByTheCompiledAdapterFamily(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent-root")
	trust, privateKey := trustRoot(t)
	data := catalogData(3)
	sentinel := errors.New("adapter protocol is too new")

	_, err := catalogreader.Read(root, trust, catalogreader.Bootstrap{
		CatalogData: data, SignatureData: sign(data, privateKey),
	}, &capabilities{err: sentinel})
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "bootstrap software catalog is unsupported") {
		t.Fatalf("error = %v", err)
	}
}

func trustRoot(t *testing.T) (publication.TrustRoot, ed25519.PrivateKey) {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{
		"temper-catalog-test": privateKey.Public().(ed25519.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust, privateKey
}

func sign(data []byte, privateKey ed25519.PrivateKey) []byte {
	return fmt.Appendf(nil, "schema: temper-signature/v1\nkey_id: temper-catalog-test\nalgorithm: ed25519\nsignature: %s\n",
		base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data)))
}

func catalogData(sequence uint64) []byte {
	return fmt.Appendf(nil, `schema: temper-software-supply/v1
sequence: %d
published_at: 2026-08-20T10:00:00Z
methods:
  system-package: {description: system package manager}
adapters:
  homebrew: {method: system-package, protocol: temper-installer-adapter/v1, effect_model: shared}
target_bindings:
  - {method: system-package, target: {os: darwin, arch: arm64}, adapter: homebrew}
packages:
  llama-cpp:
    description: primary runtime
    recipes:
      homebrew:
        method: system-package
        recipe_revision: llama-cpp/v1
        source: {kind: homebrew-formula, tap: homebrew/core, formula: llama.cpp}
        version_scheme: semver
        selection: {policy: latest, minimum_compatible: 1.0.0}
        dependencies: []
        exclude: []
        gates: [plain-completion]
        tested:
          - root_version: 1.2.3
            closure_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            target: {os: darwin, arch: arm64}
            evidence: results/llama-cpp-1.2.3
`, sequence)
}
