package catalogupdate_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogstore"
	"github.com/temper-sh/temper/internal/software/catalogupdate"
)

func TestRunVerifiesStoresAndIsSecondRunClean(t *testing.T) {
	privateKey, trust := updateTrust(t)
	source := updateSource(privateKey, 42, "first")
	root := filepath.Join(t.TempDir(), "temper-data")

	result, err := catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, source, updateRegistry(t))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Changed || result.Sequence != 42 || result.SHA256 == "" || result.ChannelKeyID != "fixture-key" || result.CatalogKeyID != "fixture-key" {
		t.Fatalf("Run() result = %+v", result)
	}
	active, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}
	if !active.Exists() || active.Catalog.SHA256 != result.SHA256 {
		t.Fatalf("active catalog = exists %v digest %q", active.Exists(), active.Catalog.SHA256)
	}

	before := updateTree(t, root)
	result, err = catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, source, updateRegistry(t))
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if result.Changed {
		t.Fatalf("second Run() result = %+v, want unchanged", result)
	}
	if after := updateTree(t, root); after != before {
		t.Errorf("clean second run mutated store\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestRunDryRunValidatesWithoutCreatingRoot(t *testing.T) {
	privateKey, trust := updateTrust(t)
	source := updateSource(privateKey, 42, "dry-run")
	root := filepath.Join(t.TempDir(), "temper-data")

	result, err := catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable", DryRun: true}, trust, source, updateRegistry(t))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Changed || !result.DryRun {
		t.Fatalf("Run() result = %+v, want changed dry-run", result)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry-run created root: %v", err)
	}
}

func TestRunRefusesBadChannelBeforeCatalogRead(t *testing.T) {
	privateKey, trust := updateTrust(t)
	source := updateSource(privateKey, 42, "bad-channel")
	source.channel.Data = append(append([]byte(nil), source.channel.Data...), '\n')
	root := filepath.Join(t.TempDir(), "temper-data")

	_, err := catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, source, updateRegistry(t))
	if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("Run() error = %v, want signature refusal", err)
	}
	if source.catalogReads != 0 {
		t.Errorf("invalid channel caused %d catalog reads", source.catalogReads)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid channel created root: %v", err)
	}
}

func TestRunRefusesRollbackAndSameSequenceEquivocation(t *testing.T) {
	privateKey, trust := updateTrust(t)
	root := filepath.Join(t.TempDir(), "temper-data")
	if _, err := catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, updateSource(privateKey, 42, "active"), updateRegistry(t)); err != nil {
		t.Fatal(err)
	}
	activeBefore, err := catalogstore.Read(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		sequence uint64
		salt     string
		want     string
	}{
		{name: "rollback", sequence: 41, salt: "older", want: "rollback refused"},
		{name: "equivocation", sequence: 42, salt: "different", want: "equivocation refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, updateSource(privateKey, tt.sequence, tt.salt), updateRegistry(t))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want %q", err, tt.want)
			}
			after, readErr := catalogstore.Read(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if after.Catalog.SHA256 != activeBefore.Catalog.SHA256 {
				t.Errorf("refusal changed active digest to %q", after.Catalog.SHA256)
			}
		})
	}
}

func TestRunRefusesUnsupportedCatalogBeforeFilesystemEffects(t *testing.T) {
	privateKey, trust := updateTrust(t)
	root := filepath.Join(t.TempDir(), "temper-data")
	emptyRegistry, err := adapter.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}

	_, err = catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, updateSource(privateKey, 42, "unsupported"), emptyRegistry)
	if err == nil || !strings.Contains(err.Error(), "not compiled into this binary") {
		t.Fatalf("Run() error = %v, want compiled capability refusal", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("capability refusal created root: %v", err)
	}
}

func TestRunRefusesTamperedActiveSignature(t *testing.T) {
	privateKey, trust := updateTrust(t)
	root := filepath.Join(t.TempDir(), "temper-data")
	result, err := catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, updateSource(privateKey, 42, "active"), updateRegistry(t))
	if err != nil {
		t.Fatal(err)
	}
	signaturePath := filepath.Join(root, "software", "catalog", "snapshots", result.SHA256, "catalog.signature.yaml")
	if err := os.WriteFile(signaturePath, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = catalogupdate.Run(context.Background(), catalogupdate.Options{Root: root, Channel: "stable"}, trust, updateSource(privateKey, 43, "candidate"), updateRegistry(t))
	if err == nil || !strings.Contains(err.Error(), "active software catalog signature") {
		t.Fatalf("Run() error = %v, want active signature refusal", err)
	}
	pointer, readErr := os.ReadFile(filepath.Join(root, "software", "catalog", "active"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(pointer) != result.SHA256+"\n" {
		t.Errorf("active signature refusal changed pointer to %q", pointer)
	}
}

type fixtureSource struct {
	channel      catalogupdate.SignedArtifact
	catalog      catalogupdate.SignedArtifact
	locator      string
	channelReads int
	catalogReads int
}

func (s *fixtureSource) Channel(_ context.Context, _ string) (catalogupdate.SignedArtifact, error) {
	s.channelReads++
	return s.channel, nil
}

func (s *fixtureSource) Catalog(_ context.Context, locator string) (catalogupdate.SignedArtifact, error) {
	s.catalogReads++
	if locator != s.locator {
		return catalogupdate.SignedArtifact{}, fmt.Errorf("unexpected locator %q", locator)
	}
	return s.catalog, nil
}

func updateSource(privateKey ed25519.PrivateKey, sequence uint64, salt string) *fixtureSource {
	catalogData := updateCatalog(sequence, salt)
	digest := catalog.SnapshotDigest(catalogData)
	locator := fmt.Sprintf("fixture://catalog/%d/%s", sequence, salt)
	channelData := []byte(fmt.Sprintf("schema: %s\nchannel: stable\ncatalog:\n  schema: %s\n  sequence: %d\n  sha256: %s\n  locator: %s\n", publication.ChannelSchemaV1, catalog.SchemaV1, sequence, digest, locator))
	return &fixtureSource{
		channel: catalogupdate.SignedArtifact{Data: channelData, Signature: updateSign(privateKey, channelData)},
		catalog: catalogupdate.SignedArtifact{Data: catalogData, Signature: updateSign(privateKey, catalogData)},
		locator: locator,
	}
}

func updateTrust(t *testing.T) (ed25519.PrivateKey, publication.TrustRoot) {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, ed25519.SeedSize))
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"fixture-key": privateKey.Public().(ed25519.PublicKey)})
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, trust
}

func updateSign(privateKey ed25519.PrivateKey, data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data))
	return []byte(fmt.Sprintf("schema: %s\nkey_id: fixture-key\nalgorithm: %s\nsignature: %s\n", publication.SignatureSchemaV1, publication.AlgorithmEd25519, encoded))
}

func updateRegistry(t *testing.T) adapter.Registry {
	t.Helper()
	registry, err := adapter.NewRegistry(adapter.Descriptor{
		ID: "homebrew", Method: "system-package", Protocol: catalog.AdapterProtocolV1, EffectModel: "shared",
		Targets: []software.Target{{OS: "darwin", Arch: "arm64"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func updateTree(t *testing.T, root string) string {
	t.Helper()
	var state strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		fmt.Fprintf(&state, "%s %s %d %d\n", strings.TrimPrefix(path, root), info.Mode(), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state.String()
}

func updateCatalog(sequence uint64, salt string) []byte {
	return []byte(fmt.Sprintf(`schema: temper-software-supply/v1
sequence: %d
published_at: 2026-08-20T18:30:00Z
methods:
  system-package: {description: Shared target package manager}
adapters:
  homebrew: {method: system-package, protocol: temper-installer-adapter/v1, effect_model: shared}
target_bindings:
  - method: system-package
    target: {os: darwin, arch: arm64}
    adapter: homebrew
packages:
  llama-cpp:
    description: Primary runtime
    recipes:
      homebrew:
        method: system-package
        recipe_revision: llama-cpp-homebrew/v1
        source: {kind: homebrew-formula, tap: homebrew/core, formula: llama.cpp}
        version_scheme: semver
        selection: {policy: latest, minimum_compatible: 1.0.0}
        dependencies: []
        exclude: []
        gates: [runtime-smoke.v1]
        tested:
          - root_version: 1.0.0
            closure_digest: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
            target: {os: darwin, arch: arm64}
            evidence: results/software/llama-cpp-1.0.0-%s
`, sequence, salt))
}
