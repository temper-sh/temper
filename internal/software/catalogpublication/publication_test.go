package publication_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
)

func TestVerifyChannelAndCatalogBindsExactSignedArtifacts(t *testing.T) {
	_, privateKey, trust := fixtureTrust(t)
	catalogData := fixtureCatalog(42)
	digest := catalog.SnapshotDigest(catalogData)
	channelData := fixtureChannel("stable", 42, digest, "fixture://catalog/42")

	channel, err := publication.VerifyChannel("stable", channelData, sign(privateKey, "fixture-key", channelData), trust)
	if err != nil {
		t.Fatalf("VerifyChannel() error = %v", err)
	}
	verified, err := publication.VerifyCatalog(channel.Document.Catalog, catalogData, sign(privateKey, "fixture-key", catalogData), trust)
	if err != nil {
		t.Fatalf("VerifyCatalog() error = %v", err)
	}
	if verified.Snapshot.SHA256 != digest || verified.Snapshot.Document.Sequence != 42 {
		t.Errorf("verified catalog = digest %q sequence %d", verified.Snapshot.SHA256, verified.Snapshot.Document.Sequence)
	}
	if channel.KeyID != "fixture-key" || verified.KeyID != "fixture-key" {
		t.Errorf("verified keys = channel %q catalog %q", channel.KeyID, verified.KeyID)
	}
}

func TestVerifyRefusesUntrustedTamperedAndStructurallyInvalidPublications(t *testing.T) {
	_, privateKey, trust := fixtureTrust(t)
	data := fixtureCatalog(42)
	digest := catalog.SnapshotDigest(data)
	reference := publication.CatalogReference{Schema: catalog.SchemaV1, Sequence: 42, SHA256: digest, Locator: "fixture://catalog/42"}

	tests := []struct {
		name      string
		data      []byte
		signature []byte
		want      string
	}{
		{
			name:      "tampered exact bytes",
			data:      append(append([]byte(nil), data...), '\n'),
			signature: sign(privateKey, "fixture-key", data),
			want:      "signature verification failed",
		},
		{
			name:      "unknown key",
			data:      data,
			signature: sign(privateKey, "retired-key", data),
			want:      "is not trusted",
		},
		{
			name: "unknown catalog field",
			data: bytes.Replace(data, []byte("sequence: 42"), []byte("sequence: 42\ncommand: dangerous"), 1),
			want: "field command not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.signature == nil {
				tt.signature = sign(privateKey, "fixture-key", tt.data)
			}
			_, err := publication.VerifyCatalog(reference, tt.data, tt.signature, trust)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("VerifyCatalog() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestVerifyRefusesChannelJoinMismatchAndUnknownFields(t *testing.T) {
	_, privateKey, trust := fixtureTrust(t)
	catalogData := fixtureCatalog(42)
	digest := catalog.SnapshotDigest(catalogData)

	unknown := fixtureChannel("stable", 42, digest, "fixture://catalog/42")
	unknown = bytes.Replace(unknown, []byte("channel: stable"), []byte("channel: stable\ncommand: dangerous"), 1)
	_, err := publication.VerifyChannel("stable", unknown, sign(privateKey, "fixture-key", unknown), trust)
	if err == nil || !strings.Contains(err.Error(), "field command not found") {
		t.Fatalf("VerifyChannel() unknown-field error = %v", err)
	}

	wrongDigest := strings.Repeat("f", 64)
	reference := publication.CatalogReference{Schema: catalog.SchemaV1, Sequence: 42, SHA256: wrongDigest, Locator: "fixture://catalog/42"}
	_, err = publication.VerifyCatalog(reference, catalogData, sign(privateKey, "fixture-key", catalogData), trust)
	if err == nil || !strings.Contains(err.Error(), "channel names") {
		t.Fatalf("VerifyCatalog() digest-join error = %v", err)
	}
}

func TestVerifyRefusesUnknownSignatureEnvelopeFields(t *testing.T) {
	_, privateKey, trust := fixtureTrust(t)
	data := fixtureCatalog(42)
	envelope := sign(privateKey, "fixture-key", data)
	envelope = bytes.Replace(envelope, []byte("algorithm: ed25519"), []byte("algorithm: ed25519\ncommand: dangerous"), 1)

	_, err := trust.Verify(data, envelope)
	if err == nil || !strings.Contains(err.Error(), "field command not found") {
		t.Fatalf("Verify() error = %v, want strict signature-envelope refusal", err)
	}
}

func fixtureTrust(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, publication.TrustRoot) {
	t.Helper()
	privateKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"fixture-key": publicKey})
	if err != nil {
		t.Fatal(err)
	}
	return publicKey, privateKey, trust
}

func sign(privateKey ed25519.PrivateKey, keyID string, data []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, data))
	return []byte(fmt.Sprintf("schema: %s\nkey_id: %s\nalgorithm: %s\nsignature: %s\n", publication.SignatureSchemaV1, keyID, publication.AlgorithmEd25519, encoded))
}

func fixtureChannel(name string, sequence uint64, digest, locator string) []byte {
	return []byte(fmt.Sprintf("schema: %s\nchannel: %s\ncatalog:\n  schema: %s\n  sequence: %d\n  sha256: %s\n  locator: %s\n", publication.ChannelSchemaV1, name, catalog.SchemaV1, sequence, digest, locator))
}

func fixtureCatalog(sequence uint64) []byte {
	return []byte(fmt.Sprintf(`schema: temper-software-supply/v1
sequence: %d
published_at: 2026-08-20T18:30:00Z
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
            evidence: results/software/llama-cpp-1.0.0
`, sequence))
}
