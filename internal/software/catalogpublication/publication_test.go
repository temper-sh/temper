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
	return []byte(fmt.Sprintf("schema: %s\nchannel: %s\ncatalog:\n  schema: %s\n  sequence: %d\n  sha256: %s\n  locator: %s\n", publication.CurrentChannelSchema, name, "temper-catalog/v3", sequence, digest, locator))
}

func TestChannelRequiresCurrentSchemaAndExactSnapshotLocator(t *testing.T) {
	_, key, trust := fixtureTrust(t)
	digest := strings.Repeat("a", 64)
	raw := fixtureChannel("stable", 42, digest, "https://example.test/snapshots/"+digest+"/")
	if _, err := publication.VerifyChannel("stable", raw, sign(key, "fixture-key", raw), trust); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]byte) []byte{
		func(b []byte) []byte {
			return bytes.Replace(b, []byte("temper-catalog/v3"), []byte("temper-catalog/v2"), 1)
		},
		func(b []byte) []byte {
			return bytes.Replace(b, []byte("channel: stable"), []byte("channel: stable\ncommand: dangerous"), 1)
		},
		func(b []byte) []byte { return bytes.Replace(b, []byte("https://"), []byte("http://"), 1) },
	} {
		b := mutate(raw)
		if _, err := publication.VerifyChannel("stable", b, sign(key, "fixture-key", b), trust); err == nil {
			t.Fatal("invalid signed channel accepted")
		}
	}
	for _, envelope := range [][]byte{sign(key, "retired", raw), sign(key, "fixture-key", append(raw, 'x'))} {
		if _, err := publication.VerifyChannel("stable", raw, envelope, trust); err == nil {
			t.Fatal("untrusted signature accepted")
		}
	}
}
