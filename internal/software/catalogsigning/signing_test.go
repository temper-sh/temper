package catalogsigning_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
)

type capabilities struct{ err error }

func (c capabilities) ValidateCatalog(catalog.Document) error { return c.err }

func TestParseSeedAcceptsOnlyCanonicalBoundedInput(t *testing.T) {
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	encoded := []byte(base64.StdEncoding.EncodeToString(seed) + "\n")
	got, err := catalogsigning.ParseSeed(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, seed) {
		t.Fatal("ParseSeed() changed seed bytes")
	}
	clear(got)

	tests := [][]byte{
		nil,
		[]byte(base64.RawStdEncoding.EncodeToString(seed)),
		[]byte(" " + base64.StdEncoding.EncodeToString(seed)),
		[]byte(base64.StdEncoding.EncodeToString(seed) + "\n\n"),
		bytes.Repeat([]byte{'a'}, catalogsigning.MaxSeedInputBytes+1),
	}
	for _, input := range tests {
		if _, err := catalogsigning.ParseSeed(input); err == nil {
			t.Fatalf("ParseSeed(%q) succeeded, want refusal", input)
		}
	}
}

func TestSignAndVerifyCatalogAndChannel(t *testing.T) {
	tool, seed := fixtureTool(t, capabilities{})
	catalogData := fixtureCatalog(1)
	catalogEnvelope, err := tool.Sign(catalogsigning.KindCatalog, "", catalogData, seed)
	if err != nil {
		t.Fatal(err)
	}
	if keyID, err := tool.Verify(catalogsigning.KindCatalog, "", catalogData, catalogEnvelope); err != nil || keyID != "fixture-key" {
		t.Fatalf("Verify(catalog) = key %q, error %v", keyID, err)
	}

	digest := catalog.SnapshotDigest(catalogData)
	channelData := fixtureChannel("stable", 1, digest)
	channelEnvelope, err := tool.Sign(catalogsigning.KindChannel, "stable", channelData, seed)
	if err != nil {
		t.Fatal(err)
	}
	if keyID, err := tool.Verify(catalogsigning.KindChannel, "stable", channelData, channelEnvelope); err != nil || keyID != "fixture-key" {
		t.Fatalf("Verify(channel) = key %q, error %v", keyID, err)
	}
}

func TestSignRefusesWrongKeyInvalidArtifactAndUnsupportedCatalog(t *testing.T) {
	tool, seed := fixtureTool(t, capabilities{})
	wrong := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	_, err := tool.Sign(catalogsigning.KindCatalog, "", fixtureCatalog(1), wrong)
	if err == nil || !strings.Contains(err.Error(), "does not match configured trust key") {
		t.Fatalf("Sign(wrong key) error = %v", err)
	}

	_, err = tool.Sign(catalogsigning.KindChannel, "stable", []byte("not: a channel\n"), seed)
	if err == nil || !strings.Contains(err.Error(), "decode catalog channel") {
		t.Fatalf("Sign(invalid channel) error = %v", err)
	}

	unsupported, _ := fixtureTool(t, capabilities{err: errors.New("missing adapter")})
	_, err = unsupported.Sign(catalogsigning.KindCatalog, "", fixtureCatalog(1), seed)
	if err == nil || !strings.Contains(err.Error(), "unsupported by this release tool") {
		t.Fatalf("Sign(unsupported catalog) error = %v", err)
	}
}

func fixtureTool(t *testing.T, validator capabilities) (catalogsigning.Tool, []byte) {
	t.Helper()
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"fixture-key": publicKey})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := catalogsigning.New("fixture-key", trust, validator)
	if err != nil {
		t.Fatal(err)
	}
	return tool, seed
}

func fixtureChannel(name string, sequence uint64, digest string) []byte {
	return []byte(fmt.Sprintf("schema: temper-software-channel/v1\nchannel: %s\ncatalog:\n  schema: temper-software-supply/v1\n  sequence: %d\n  sha256: %s\n  locator: https://example.test/catalog/%s/\n", name, sequence, digest, digest))
}

func fixtureCatalog(sequence uint64) []byte {
	return []byte(fmt.Sprintf(`schema: temper-software-supply/v1
sequence: %d
published_at: 2026-08-24T17:36:04Z
methods:
  system-package:
    description: Shared target package manager
adapters:
  homebrew:
    method: system-package
    protocol: temper-installer-adapter/v1
    effect_model: shared
target_bindings:
  - method: system-package
    target: {os: darwin, arch: arm64}
    adapter: homebrew
packages:
  fixture:
    description: Fixture package
    recipes:
      homebrew:
        method: system-package
        recipe_revision: fixture-homebrew/v1
        source: {kind: homebrew-formula, tap: homebrew/core, formula: fixture}
        version_scheme: semver
        selection: {policy: exact, exact: 1.0.0}
        dependencies: []
        exclude: []
        gates: [fixture-smoke.v1]
        tested: []
`, sequence))
}
