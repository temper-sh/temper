// Package fieldkitbinding builds and validates the canonical Temper identity
// document carried by a Field Kit packet. It is a pure computation: callers
// supply already-read bytes, locks, receipts, and machine facts.
package fieldkitbinding

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	manifestlock "github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/machine"
	"github.com/temper-sh/temper/internal/software/installplan"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = "temper-field-kit-binding/v1"

var (
	idPattern     = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Inputs contains the complete pure input to one binding. Installation order
// is significant and is preserved in the resulting document.
type Inputs struct {
	TemperBinary       []byte
	Machine            machine.Facts
	ManifestLock       []byte
	RenderedGeneration string
	Installations      []InstallationInput
}

// InstallationInput pairs an exact desired software lock with the canonical
// observed receipt for one named installation.
type InstallationInput struct {
	Lock        softwarelock.Document
	ReceiptData []byte
}

type Document struct {
	Schema             string                 `yaml:"schema"`
	TemperBinary       BinaryIdentity         `yaml:"temper_binary"`
	Machine            machine.Facts          `yaml:"machine"`
	ManifestLock       ManifestLockIdentity   `yaml:"manifest_lock"`
	RenderedGeneration GenerationIdentity     `yaml:"rendered_generation"`
	Installations      []InstallationIdentity `yaml:"installations"`
}

type BinaryIdentity struct {
	OS     string `yaml:"os"`
	Arch   string `yaml:"arch"`
	SHA256 string `yaml:"sha256"`
}

type ManifestLockIdentity struct {
	Schema string `yaml:"schema"`
	SHA256 string `yaml:"sha256"`
}

type GenerationIdentity struct {
	SHA256 string `yaml:"sha256"`
}

// InstallationIdentity is recursive so a packet never relies on an implicit
// receipt lookup to discover the base identities an experiment required.
type InstallationIdentity struct {
	Installation       string                 `yaml:"installation"`
	SoftwareLockDigest string                 `yaml:"software_lock_digest"`
	ReceiptSHA256      string                 `yaml:"receipt_sha256"`
	Requirements       []InstallationIdentity `yaml:"requirements"`
}

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "field-kit binding invalid: " + strings.Join(e.Problems, "; ")
}

// Build verifies every supplied identity and composes the recursively explicit
// ordered installation set. It performs no reads or effects.
func Build(inputs Inputs) (Document, error) {
	if len(inputs.TemperBinary) == 0 {
		return Document{}, errors.New("field-kit binding requires exact Temper binary bytes")
	}
	if err := inputs.Machine.Validate(); err != nil {
		return Document{}, err
	}
	if inputs.Machine.Target.OS != "darwin" || inputs.Machine.Target.Arch != "arm64" || inputs.Machine.Target.Distribution != "macos" {
		return Document{}, errors.New("field-kit binding v1 requires a darwin/arm64 macOS machine")
	}
	lockedManifest, err := manifestlock.Parse(inputs.ManifestLock)
	if err != nil {
		return Document{}, fmt.Errorf("field-kit binding manifest lock: %w", err)
	}
	if !sha256Pattern.MatchString(inputs.RenderedGeneration) {
		return Document{}, errors.New("field-kit binding rendered generation must be 64 lowercase hexadecimal characters")
	}
	if len(inputs.Installations) == 0 {
		return Document{}, errors.New("field-kit binding requires at least one software installation")
	}

	document := Document{
		Schema: SchemaV1,
		TemperBinary: BinaryIdentity{
			OS: inputs.Machine.Target.OS, Arch: inputs.Machine.Target.Arch,
			SHA256: Digest(inputs.TemperBinary),
		},
		Machine: inputs.Machine,
		ManifestLock: ManifestLockIdentity{
			Schema: lockedManifest.Schema, SHA256: Digest(inputs.ManifestLock),
		},
		RenderedGeneration: GenerationIdentity{SHA256: inputs.RenderedGeneration},
		Installations:      make([]InstallationIdentity, 0, len(inputs.Installations)),
	}

	type observedInstallation struct {
		identity InstallationIdentity
		receipt  receipt.Document
	}
	previous := make([]observedInstallation, 0, len(inputs.Installations))
	root := ""
	for index, input := range inputs.Installations {
		if err := input.Lock.Validate(); err != nil {
			return Document{}, fmt.Errorf("field-kit binding installations[%d] software lock: %w", index, err)
		}
		receipted, err := receipt.Parse(input.ReceiptData)
		if err != nil {
			return Document{}, fmt.Errorf("field-kit binding installations[%d] receipt: %w", index, err)
		}
		installation := installplan.Installation{ID: receipted.Installation, Root: receipted.Root}
		if err := receipted.ValidateAgainst(input.Lock, installation); err != nil {
			return Document{}, fmt.Errorf("field-kit binding installation %q: %w", receipted.Installation, err)
		}
		if input.Lock.Target != inputs.Machine.Target || receipted.Target != inputs.Machine.Target {
			return Document{}, fmt.Errorf("field-kit binding installation %q target differs from machine facts", receipted.Installation)
		}
		if root == "" {
			root = receipted.Root
		} else if receipted.Root != root {
			return Document{}, fmt.Errorf("field-kit binding installation %q belongs to another Temper root", receipted.Installation)
		}
		lockDigest, err := input.Lock.SemanticDigest()
		if err != nil {
			return Document{}, fmt.Errorf("field-kit binding installation %q software lock: %w", receipted.Installation, err)
		}
		identity := InstallationIdentity{
			Installation: receipted.Installation, SoftwareLockDigest: lockDigest,
			ReceiptSHA256: receipt.Digest(input.ReceiptData),
			Requirements:  make([]InstallationIdentity, 0, len(receipted.Requirements)),
		}
		for _, requirement := range receipted.Requirements {
			matched := false
			for _, candidate := range previous {
				if candidate.receipt.Installation != requirement.Installation ||
					candidate.identity.SoftwareLockDigest != requirement.SoftwareLockDigest ||
					candidate.identity.ReceiptSHA256 != requirement.ReceiptSHA256 {
					continue
				}
				identity.Requirements = append(identity.Requirements, cloneIdentity(candidate.identity))
				matched = true
				break
			}
			if !matched {
				return Document{}, fmt.Errorf(
					"field-kit binding installation %q required receipt identity %s/%s@%s is not an earlier installation",
					receipted.Installation, requirement.Installation, requirement.SoftwareLockDigest, requirement.ReceiptSHA256,
				)
			}
		}
		document.Installations = append(document.Installations, identity)
		previous = append(previous, observedInstallation{identity: cloneIdentity(identity), receipt: receipted})
	}
	if err := document.Validate(); err != nil {
		return Document{}, err
	}
	return document, nil
}

// Parse accepts only the canonical YAML bytes produced by Marshal.
func Parse(data []byte) (Document, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return Document{}, fmt.Errorf("decode field-kit binding: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Document{}, errors.New("decode field-kit binding: multiple YAML documents are not allowed")
		}
		return Document{}, fmt.Errorf("decode field-kit binding: %w", err)
	}
	canonical, err := Marshal(document)
	if err != nil {
		return Document{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Document{}, errors.New("field-kit binding bytes are not canonical")
	}
	return document, nil
}

func Marshal(document Document) ([]byte, error) {
	if err := document.Validate(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("encode field-kit binding: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("close field-kit binding encoder: %w", err)
	}
	return output.Bytes(), nil
}

func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (d Document) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	if d.Schema != SchemaV1 {
		problem("schema is %q, want %q", d.Schema, SchemaV1)
	}
	if err := d.Machine.Validate(); err != nil {
		problem("machine: %v", err)
	}
	if d.Machine.Target.OS != "darwin" || d.Machine.Target.Arch != "arm64" || d.Machine.Target.Distribution != "macos" {
		problem("machine target must identify darwin/arm64 macOS")
	}
	if d.TemperBinary.OS != "darwin" || d.TemperBinary.Arch != "arm64" {
		problem("temper_binary must identify darwin/arm64")
	}
	if d.TemperBinary.OS != d.Machine.Target.OS || d.TemperBinary.Arch != d.Machine.Target.Arch {
		problem("temper_binary target differs from machine target")
	}
	if !sha256Pattern.MatchString(d.TemperBinary.SHA256) {
		problem("temper_binary.sha256 must be 64 lowercase hexadecimal characters")
	}
	if d.ManifestLock.Schema != manifestlock.SchemaV1 {
		problem("manifest_lock.schema is %q, want %q", d.ManifestLock.Schema, manifestlock.SchemaV1)
	}
	if !sha256Pattern.MatchString(d.ManifestLock.SHA256) {
		problem("manifest_lock.sha256 must be 64 lowercase hexadecimal characters")
	}
	if !sha256Pattern.MatchString(d.RenderedGeneration.SHA256) {
		problem("rendered_generation.sha256 must be 64 lowercase hexadecimal characters")
	}
	if len(d.Installations) == 0 {
		problem("installations must not be empty")
	}
	seenInstallations := map[string]bool{}
	previous := make([]InstallationIdentity, 0, len(d.Installations))
	for index, identity := range d.Installations {
		location := fmt.Sprintf("installations[%d]", index)
		if !idPattern.MatchString(identity.Installation) {
			problem("%s.installation %q is not a lowercase stable id", location, identity.Installation)
		}
		if seenInstallations[identity.Installation] {
			problem("installations repeats installation %q", identity.Installation)
		}
		seenInstallations[identity.Installation] = true
		if !sha256Pattern.MatchString(identity.SoftwareLockDigest) {
			problem("%s.software_lock_digest must be 64 lowercase hexadecimal characters", location)
		}
		if !sha256Pattern.MatchString(identity.ReceiptSHA256) {
			problem("%s.receipt_sha256 must be 64 lowercase hexadecimal characters", location)
		}
		previousDigest := ""
		for requirementIndex, requirement := range identity.Requirements {
			if previousDigest != "" && requirement.SoftwareLockDigest <= previousDigest {
				problem("%s.requirements must be unique and sorted by software_lock_digest", location)
			}
			previousDigest = requirement.SoftwareLockDigest
			matched := false
			for _, candidate := range previous {
				if identitiesEqual(requirement, candidate) {
					matched = true
					break
				}
			}
			if !matched {
				problem("%s.requirements[%d] must equal one complete earlier identity", location, requirementIndex)
			}
		}
		previous = append(previous, cloneIdentity(identity))
	}
	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func cloneIdentity(identity InstallationIdentity) InstallationIdentity {
	cloned := identity
	cloned.Requirements = make([]InstallationIdentity, len(identity.Requirements))
	for index, requirement := range identity.Requirements {
		cloned.Requirements[index] = cloneIdentity(requirement)
	}
	return cloned
}

func identitiesEqual(left, right InstallationIdentity) bool {
	if left.Installation != right.Installation || left.SoftwareLockDigest != right.SoftwareLockDigest || left.ReceiptSHA256 != right.ReceiptSHA256 || len(left.Requirements) != len(right.Requirements) {
		return false
	}
	for index := range left.Requirements {
		if !identitiesEqual(left.Requirements[index], right.Requirements[index]) {
			return false
		}
	}
	return true
}
