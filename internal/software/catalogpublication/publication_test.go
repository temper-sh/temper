package publication_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
)

func TestVerifyRefusesUnknownSignatureEnvelopeFields(t *testing.T) {
	_, privateKey, trust := fixtureTrust(t)
	data := []byte("signed fixture")
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
	return []byte(fmt.Sprintf("schema: %s\nchannel: %s\nsequence: %d\ncatalogs:\n  apple-silicon:\n    schema: %s\n    sha256: %s\n    locator: %s\n", publication.CurrentChannelSchema, name, sequence, "temper-catalog/v1", digest, locator))
}

func TestChannelRequiresCurrentSchemaAndExactSnapshotLocator(t *testing.T) {
	_, key, trust := fixtureTrust(t)
	digest := strings.Repeat("a", 64)
	raw := fixtureChannel("stable", 42, digest, "https://example.test/snapshots/"+digest+"/")
	read := func(b []byte) error {
		verified, err := publication.VerifyChannel("stable", b, sign(key, "fixture-key", b), trust)
		if err == nil {
			_, err = verified.Document.Catalog("apple-silicon")
		}
		return err
	}
	if err := read(raw); err != nil {
		t.Fatal(err)
	}
	for want, mutate := range map[string]func([]byte) []byte{
		"needs a newer Temper": func(b []byte) []byte {
			return bytes.Replace(b, []byte("temper-catalog/v1"), []byte("temper-catalog/v2"), 1)
		},
		"command": func(b []byte) []byte {
			return bytes.Replace(b, []byte("channel: stable"), []byte("channel: stable\ncommand: dangerous"), 1)
		},
		"HTTPS":               func(b []byte) []byte { return bytes.Replace(b, []byte("https://"), []byte("http://"), 1) },
		"field future":        func(b []byte) []byte { return append(b, []byte("    future: true\n")...) },
		"no catalog for this": func(b []byte) []byte { return bytes.Replace(b, []byte("apple-silicon:"), []byte("linux-cuda:"), 1) },
	} {
		if err := read(mutate(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", want, err)
		}
	}
	// Another platform's entry is not this client's to read.
	other := append(append([]byte(nil), raw...), []byte("  linux-cuda:\n    schema: temper-catalog/v9\n    future: true\n")...)
	if err := read(other); err != nil {
		t.Fatalf("another platform's entry broke this client: %v", err)
	}
	verified, err := publication.VerifyChannel("stable", other, sign(key, "fixture-key", other), trust)
	if err != nil {
		t.Fatal(err)
	}
	next, err := verified.Document.WithCatalog("apple-silicon", publication.CatalogReference{Schema: "temper-catalog/v1", SHA256: strings.Repeat("b", 64), Locator: "https://example.test/snapshots/" + strings.Repeat("b", 64) + "/"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := publication.MarshalChannel(next)
	if err != nil || !strings.Contains(string(encoded), "future: true") || !strings.Contains(string(encoded), strings.Repeat("b", 64)) {
		t.Fatalf("next channel lost an entry or the replacement:\n%s %v", encoded, err)
	}
	for _, envelope := range [][]byte{sign(key, "retired", raw), sign(key, "fixture-key", append(raw, 'x'))} {
		if _, err := publication.VerifyChannel("stable", raw, envelope, trust); err == nil {
			t.Fatal("untrusted signature accepted")
		}
	}
}
