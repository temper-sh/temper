package catalogsigning_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"

	"fmt"
	"os"
	"strings"
	"testing"

	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
)

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
	tool, seed := fixtureTool(t)
	catalogData := fixtureCatalog(t)
	catalogEnvelope, err := tool.Sign(catalogsigning.KindCatalog, "", catalogData, seed)
	if err != nil {
		t.Fatal(err)
	}
	if keyID, err := tool.Verify(catalogsigning.KindCatalog, "", catalogData, catalogEnvelope); err != nil || keyID != "fixture-key" {
		t.Fatalf("Verify(catalog) = key %q, error %v", keyID, err)
	}

	digest := fmt.Sprintf("%x", sha256.Sum256(catalogData))
	channelData := fixtureChannel("stable", 1, digest)
	channelEnvelope, err := tool.Sign(catalogsigning.KindChannel, "stable", channelData, seed)
	if err != nil {
		t.Fatal(err)
	}
	if keyID, err := tool.Verify(catalogsigning.KindChannel, "stable", channelData, channelEnvelope); err != nil || keyID != "fixture-key" {
		t.Fatalf("Verify(channel) = key %q, error %v", keyID, err)
	}
}

func TestCurrentCatalogSigningUsesItsCompiledCapabilities(t *testing.T) {
	tool, seed := fixtureTool(t)
	data, err := os.ReadFile("../../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := tool.Sign(catalogsigning.KindCatalog, "", data, seed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tool.Verify(catalogsigning.KindCatalog, "", data, signature); err != nil {
		t.Fatal(err)
	}
	unsupported := bytes.Replace(data, []byte("llama-server/v2"), []byte("unsupported-engine/v9"), 1)
	if _, err := tool.Sign(catalogsigning.KindCatalog, "", unsupported, seed); err == nil {
		t.Fatal("signed an unsupported current engine adapter")
	}
}

func TestSignRefusesWrongKeyInvalidArtifactAndUnsupportedCatalog(t *testing.T) {
	tool, seed := fixtureTool(t)
	wrong := bytes.Repeat([]byte{9}, ed25519.SeedSize)
	_, err := tool.Sign(catalogsigning.KindCatalog, "", fixtureCatalog(t), wrong)
	if err == nil || !strings.Contains(err.Error(), "does not match configured trust key") {
		t.Fatalf("Sign(wrong key) error = %v", err)
	}

	_, err = tool.Sign(catalogsigning.KindChannel, "stable", []byte("not: a channel\n"), seed)
	if err == nil || !strings.Contains(err.Error(), "decode catalog channel") {
		t.Fatalf("Sign(invalid channel) error = %v", err)
	}

}

func fixtureTool(t *testing.T) (catalogsigning.Tool, []byte) {
	t.Helper()
	seed := bytes.Repeat([]byte{7}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{"fixture-key": publicKey})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := catalogsigning.New("fixture-key", trust)
	if err != nil {
		t.Fatal(err)
	}
	return tool, seed
}

func fixtureChannel(name string, sequence uint64, digest string) []byte {
	return []byte(fmt.Sprintf("schema: temper-catalog-channel/v1\nchannel: %s\ncatalog:\n  schema: temper-catalog/v3\n  sequence: %d\n  sha256: %s\n  locator: https://example.test/snapshots/%s/\n", name, sequence, digest, digest))
}

func fixtureCatalog(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../../catalog/guided-setup.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
