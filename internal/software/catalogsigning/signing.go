// Package catalogsigning owns the pure release-side signing and verification
// policy for software catalog publications. Private key material enters only
// as a caller-owned seed and is never retained by the tool.
package catalogsigning

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
)

const MaxSeedInputBytes = 128

type Kind string

const (
	KindCatalog Kind = "catalog"
	KindChannel Kind = "channel"
)

type CapabilityValidator interface {
	ValidateCatalog(catalog.Document) error
}

type Tool struct {
	keyID        string
	trust        publication.TrustRoot
	capabilities CapabilityValidator
}

func New(keyID string, trust publication.TrustRoot, capabilities CapabilityValidator) (Tool, error) {
	if keyID == "" {
		return Tool{}, errors.New("catalog signing key id is required")
	}
	if capabilities == nil {
		return Tool{}, errors.New("catalog signing capability validator is required")
	}
	return Tool{keyID: keyID, trust: trust, capabilities: capabilities}, nil
}

func ParseKind(value string) (Kind, error) {
	switch Kind(value) {
	case KindCatalog, KindChannel:
		return Kind(value), nil
	default:
		return "", fmt.Errorf("catalog publication kind %q must be catalog or channel", value)
	}
}

// ParseSeed accepts canonical standard padded base64 with at most one final
// LF or CRLF. It never includes the supplied bytes in an error.
func ParseSeed(input []byte) ([]byte, error) {
	if len(input) > MaxSeedInputBytes {
		return nil, errors.New("catalog signing seed input exceeds the size limit")
	}
	encoded := input
	if bytes.HasSuffix(encoded, []byte("\r\n")) {
		encoded = encoded[:len(encoded)-2]
	} else if bytes.HasSuffix(encoded, []byte("\n")) {
		encoded = encoded[:len(encoded)-1]
	}
	if len(encoded) == 0 {
		return nil, errors.New("catalog signing seed is required on stdin")
	}
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Strict().Decode(decoded, encoded)
	if err != nil {
		clear(decoded)
		return nil, errors.New("catalog signing seed must be canonical standard padded base64")
	}
	decoded = decoded[:n]
	if len(decoded) != ed25519.SeedSize {
		clear(decoded)
		return nil, fmt.Errorf("catalog signing seed has decoded length %d, want %d", n, ed25519.SeedSize)
	}
	canonical := make([]byte, base64.StdEncoding.EncodedLen(len(decoded)))
	base64.StdEncoding.Encode(canonical, decoded)
	canonicalMatch := bytes.Equal(canonical, encoded)
	clear(canonical)
	if !canonicalMatch {
		clear(decoded)
		return nil, errors.New("catalog signing seed must be canonical standard padded base64")
	}
	return decoded, nil
}

func (t Tool) Sign(kind Kind, channel string, artifact, seed []byte) ([]byte, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("catalog signing seed has decoded length %d, want %d", len(seed), ed25519.SeedSize)
	}
	if err := t.validateArtifact(kind, channel, artifact, nil); err != nil {
		return nil, err
	}

	privateKey := ed25519.NewKeyFromSeed(seed)
	defer clear(privateKey)
	signed := ed25519.Sign(privateKey, artifact)
	defer clear(signed)
	envelope := formatEnvelope(t.keyID, signed)
	if _, err := t.Verify(kind, channel, artifact, envelope); err != nil {
		return nil, fmt.Errorf("signing seed does not match configured trust key %q: %w", t.keyID, err)
	}
	return envelope, nil
}

func (t Tool) Verify(kind Kind, channel string, artifact, envelope []byte) (string, error) {
	keyID, err := t.trust.Verify(artifact, envelope)
	if err != nil {
		return "", err
	}
	if keyID != t.keyID {
		return "", fmt.Errorf("catalog publication uses trust key %q, want release key %q", keyID, t.keyID)
	}
	if err := t.validateArtifact(kind, channel, artifact, envelope); err != nil {
		return "", err
	}
	return keyID, nil
}

func (t Tool) validateArtifact(kind Kind, channel string, artifact, envelope []byte) error {
	switch kind {
	case KindCatalog:
		if channel != "" {
			return errors.New("--channel is valid only for channel publications")
		}
		snapshot, err := catalog.ParseSnapshot(artifact)
		if err != nil {
			return err
		}
		if err := t.capabilities.ValidateCatalog(snapshot.Document); err != nil {
			return fmt.Errorf("software catalog is unsupported by this release tool: %w", err)
		}
		return nil
	case KindChannel:
		if err := publication.ValidateChannelName(channel); err != nil {
			return err
		}
		if envelope == nil {
			return publication.ValidateChannel(channel, artifact)
		}
		_, err := publication.VerifyChannel(channel, artifact, envelope, t.trust)
		return err
	default:
		return fmt.Errorf("unsupported catalog publication kind %q", kind)
	}
}

func formatEnvelope(keyID string, signature []byte) []byte {
	return fmt.Appendf(nil, "schema: %s\nkey_id: %s\nalgorithm: %s\nsignature: %s\n",
		publication.SignatureSchemaV1,
		keyID,
		publication.AlgorithmEd25519,
		base64.StdEncoding.EncodeToString(signature),
	)
}
