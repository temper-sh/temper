// Package publication verifies the signed publication envelope around a
// software-supply catalog. Artifact bytes remain exact until their detached
// signature and channel join have both been proven.
package publication

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/temper-sh/temper/internal/software/catalog"
	"gopkg.in/yaml.v3"
)

const (
	SignatureSchemaV1 = "temper-signature/v1"
	ChannelSchemaV1   = "temper-software-channel/v1"
	AlgorithmEd25519  = "ed25519"
)

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Signature is the strict detached-signature envelope. Signature contains
// standard padded base64; it signs the exact bytes of the adjacent artifact.
type Signature struct {
	Schema    string `yaml:"schema"`
	KeyID     string `yaml:"key_id"`
	Algorithm string `yaml:"algorithm"`
	Signature string `yaml:"signature"`
}

// CatalogReference is the authenticated channel-to-catalog join.
type CatalogReference struct {
	Schema   string `yaml:"schema"`
	Sequence uint64 `yaml:"sequence"`
	SHA256   string `yaml:"sha256"`
	Locator  string `yaml:"locator"`
}

type Channel struct {
	Schema  string           `yaml:"schema"`
	Channel string           `yaml:"channel"`
	Catalog CatalogReference `yaml:"catalog"`
}

type VerifiedChannel struct {
	Document Channel
	KeyID    string
}

type VerifiedCatalog struct {
	Snapshot catalog.Snapshot
	KeyID    string
}

// TrustRoot is an immutable set of catalog publication keys selected by
// stable key ID. Key rotation is a binary/release decision, not catalog data.
type TrustRoot struct {
	keys map[string]ed25519.PublicKey
}

func NewTrustRoot(keys map[string]ed25519.PublicKey) (TrustRoot, error) {
	if len(keys) == 0 {
		return TrustRoot{}, errors.New("catalog trust root must contain at least one key")
	}
	ids := make([]string, 0, len(keys))
	for id := range keys {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	cloned := make(map[string]ed25519.PublicKey, len(keys))
	for _, id := range ids {
		key := keys[id]
		if !idPattern.MatchString(id) {
			return TrustRoot{}, fmt.Errorf("catalog trust key id %q is not a lowercase stable id", id)
		}
		if len(key) != ed25519.PublicKeySize {
			return TrustRoot{}, fmt.Errorf("catalog trust key %q has length %d, want %d", id, len(key), ed25519.PublicKeySize)
		}
		cloned[id] = append(ed25519.PublicKey(nil), key...)
	}
	return TrustRoot{keys: cloned}, nil
}

// Verify proves a detached signature over exact artifact bytes. It parses
// only the signature envelope before the cryptographic check.
func (t TrustRoot) Verify(artifact, envelope []byte) (string, error) {
	signature, decoded, err := parseSignature(envelope)
	if err != nil {
		return "", err
	}
	key, ok := t.keys[signature.KeyID]
	if !ok {
		return "", fmt.Errorf("catalog signature key %q is not trusted", signature.KeyID)
	}
	if !ed25519.Verify(key, artifact, decoded) {
		return "", errors.New("catalog signature verification failed")
	}
	return signature.KeyID, nil
}

func ValidateChannelName(name string) error {
	if !idPattern.MatchString(name) {
		return fmt.Errorf("catalog channel %q is not a lowercase stable id", name)
	}
	return nil
}

func (r CatalogReference) Validate() error {
	if r.Schema != catalog.SchemaV1 {
		return fmt.Errorf("catalog reference schema is %q, want %q", r.Schema, catalog.SchemaV1)
	}
	if r.Sequence == 0 {
		return errors.New("catalog reference sequence must be greater than zero")
	}
	if !sha256Pattern.MatchString(r.SHA256) {
		return errors.New("catalog reference sha256 must be 64 lowercase hexadecimal characters")
	}
	if strings.TrimSpace(r.Locator) == "" || strings.TrimSpace(r.Locator) != r.Locator {
		return errors.New("catalog reference locator must be nonempty and trimmed")
	}
	for _, value := range r.Locator {
		if unicode.IsControl(value) {
			return errors.New("catalog reference locator must not contain control characters")
		}
	}
	return nil
}

func (c Channel) Validate() error {
	if c.Schema != ChannelSchemaV1 {
		return fmt.Errorf("catalog channel schema is %q, want %q", c.Schema, ChannelSchemaV1)
	}
	if err := ValidateChannelName(c.Channel); err != nil {
		return err
	}
	if err := c.Catalog.Validate(); err != nil {
		return err
	}
	return nil
}

// ValidateChannel parses and validates exact channel document bytes before a
// release tool signs them. Signature verification remains VerifyChannel's job.
func ValidateChannel(expected string, data []byte) error {
	if err := ValidateChannelName(expected); err != nil {
		return err
	}
	document, err := parseChannel(data)
	if err != nil {
		return err
	}
	if document.Channel != expected {
		return fmt.Errorf("catalog channel is %q, requested %q", document.Channel, expected)
	}
	return nil
}

func VerifyChannel(expected string, data, signature []byte, trust TrustRoot) (VerifiedChannel, error) {
	if err := ValidateChannelName(expected); err != nil {
		return VerifiedChannel{}, err
	}
	keyID, err := trust.Verify(data, signature)
	if err != nil {
		return VerifiedChannel{}, fmt.Errorf("verify catalog channel signature: %w", err)
	}
	document, err := parseChannel(data)
	if err != nil {
		return VerifiedChannel{}, err
	}
	if document.Channel != expected {
		return VerifiedChannel{}, fmt.Errorf("catalog channel is %q, requested %q", document.Channel, expected)
	}
	return VerifiedChannel{Document: document, KeyID: keyID}, nil
}

func VerifyCatalog(reference CatalogReference, data, signature []byte, trust TrustRoot) (VerifiedCatalog, error) {
	if err := reference.Validate(); err != nil {
		return VerifiedCatalog{}, err
	}
	keyID, err := trust.Verify(data, signature)
	if err != nil {
		return VerifiedCatalog{}, fmt.Errorf("verify software catalog signature: %w", err)
	}
	snapshot, err := catalog.ParseSnapshot(data)
	if err != nil {
		return VerifiedCatalog{}, err
	}
	if snapshot.SHA256 != reference.SHA256 {
		return VerifiedCatalog{}, fmt.Errorf("software catalog digest is %q, channel names %q", snapshot.SHA256, reference.SHA256)
	}
	if snapshot.Document.Schema != reference.Schema {
		return VerifiedCatalog{}, fmt.Errorf("software catalog schema is %q, channel names %q", snapshot.Document.Schema, reference.Schema)
	}
	if snapshot.Document.Sequence != reference.Sequence {
		return VerifiedCatalog{}, fmt.Errorf("software catalog sequence is %d, channel names %d", snapshot.Document.Sequence, reference.Sequence)
	}
	return VerifiedCatalog{Snapshot: snapshot, KeyID: keyID}, nil
}

func parseSignature(data []byte) (Signature, []byte, error) {
	var signature Signature
	if err := decodeStrict(data, &signature, "catalog signature"); err != nil {
		return Signature{}, nil, err
	}
	if signature.Schema != SignatureSchemaV1 {
		return Signature{}, nil, fmt.Errorf("catalog signature schema is %q, want %q", signature.Schema, SignatureSchemaV1)
	}
	if !idPattern.MatchString(signature.KeyID) {
		return Signature{}, nil, fmt.Errorf("catalog signature key id %q is not a lowercase stable id", signature.KeyID)
	}
	if signature.Algorithm != AlgorithmEd25519 {
		return Signature{}, nil, fmt.Errorf("catalog signature algorithm is %q, want %q", signature.Algorithm, AlgorithmEd25519)
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(signature.Signature)
	if err != nil {
		return Signature{}, nil, fmt.Errorf("decode catalog signature: %w", err)
	}
	if len(decoded) != ed25519.SignatureSize {
		return Signature{}, nil, fmt.Errorf("catalog signature has length %d, want %d", len(decoded), ed25519.SignatureSize)
	}
	if base64.StdEncoding.EncodeToString(decoded) != signature.Signature {
		return Signature{}, nil, errors.New("catalog signature must use canonical standard padded base64")
	}
	return signature, decoded, nil
}

func parseChannel(data []byte) (Channel, error) {
	var channel Channel
	if err := decodeStrict(data, &channel, "catalog channel"); err != nil {
		return Channel{}, err
	}
	if err := channel.Validate(); err != nil {
		return Channel{}, err
	}
	return channel, nil
}

func decodeStrict(data []byte, destination any, name string) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s: multiple YAML documents are not allowed", name)
		}
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}
