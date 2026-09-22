// Package distribution retrieves and retains authenticated current catalogs.
// Publication verification is pure; updates and deliberate rollback commit only
// the active catalog state, never a selection, execution lock or installation.
package distribution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

const Channel = "stable"

type Publication struct {
	Channel publication.SignedArtifact
	Catalog publication.SignedArtifact
}

type Identity struct {
	SHA256   string `json:"sha256"`
	Sequence uint64 `json:"sequence"`
}

type Snapshot struct {
	Identity
	Document     catalog.Document
	ChannelKeyID string
	CatalogKeyID string
	publication  Publication
}

// Source is the four bounded reads needed by the current publication format.
type Source interface {
	Channel(context.Context, string) (publication.SignedArtifact, error)
	CatalogJSON(context.Context, string) (publication.SignedArtifact, error)
}

type Result struct {
	Changed        bool     `json:"changed"`
	DryRun         bool     `json:"dry_run"`
	PreviousSHA256 string   `json:"previous_sha256,omitempty"`
	Active         Identity `json:"active"`
	Latest         Identity `json:"latest"`
}

// Verify binds exact catalog bytes to a signed channel sequence. It does not
// read a URL, filesystem, upstream release, or machine state.
func Verify(p Publication, trust publication.TrustRoot) (Snapshot, error) {
	channel, err := verifyChannel(p.Channel, trust)
	if err != nil {
		return Snapshot{}, err
	}
	ref := channel.Document.Catalog
	if len(p.Catalog.Data) > catalogsource.MaxCatalogBytes || len(p.Catalog.Signature) > catalogsource.MaxSignatureBytes {
		return Snapshot{}, errors.New("catalog publication exceeds its size limit")
	}
	key, err := trust.Verify(p.Catalog.Data, p.Catalog.Signature)
	if err != nil {
		return Snapshot{}, fmt.Errorf("verify catalog signature: %w", err)
	}
	if digest(p.Catalog.Data) != ref.SHA256 {
		return Snapshot{}, errors.New("catalog bytes do not match the signed channel digest")
	}
	d, err := catalog.Parse(p.Catalog.Data)
	if err != nil {
		return Snapshot{}, err
	}
	if d.Schema != ref.Schema {
		return Snapshot{}, errors.New("catalog schema does not match the signed channel")
	}
	return Snapshot{
		Identity: Identity{SHA256: ref.SHA256, Sequence: ref.Sequence},
		Document: d, ChannelKeyID: channel.KeyID, CatalogKeyID: key,
		publication: Publication{
			Channel: cloneArtifact(p.Channel), Catalog: cloneArtifact(p.Catalog),
		},
	}, nil
}

func verifyChannel(artifact publication.SignedArtifact, trust publication.TrustRoot) (publication.VerifiedChannel, error) {
	if len(artifact.Data) > catalogsource.MaxChannelBytes || len(artifact.Signature) > catalogsource.MaxSignatureBytes {
		return publication.VerifiedChannel{}, errors.New("catalog channel exceeds its size limit")
	}
	c, err := publication.VerifyChannel(Channel, artifact.Data, artifact.Signature, trust)
	if err != nil {
		return c, err
	}
	if c.Document.Schema != publication.CurrentChannelSchema || c.Document.Catalog.Schema != catalog.Schema {
		return c, errors.New("channel does not publish the supported current catalog format")
	}
	return c, nil
}

func Update(ctx context.Context, root string, dry bool, trust publication.TrustRoot, source Source) (Result, error) {
	if source == nil {
		return Result{}, errors.New("catalog source is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	observed, err := readStore(root, trust)
	if err != nil {
		return Result{}, err
	}
	channel, err := source.Channel(ctx, Channel)
	if err != nil {
		return Result{}, fmt.Errorf("read catalog channel: %w", err)
	}
	verified, err := verifyChannel(channel, trust)
	if err != nil {
		return Result{}, err
	}
	ref := verified.Document.Catalog
	if err := checkForward(observed.latest.Identity, Identity{SHA256: ref.SHA256, Sequence: ref.Sequence}); err != nil {
		return Result{}, err
	}
	data, err := source.CatalogJSON(ctx, ref.Locator)
	if err != nil {
		return Result{}, fmt.Errorf("read catalog snapshot: %w", err)
	}
	candidate, err := Verify(Publication{Channel: channel, Catalog: data}, trust)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{Changed: observed.active.SHA256 != candidate.SHA256,
		DryRun: dry, PreviousSHA256: observed.active.SHA256, Active: candidate.Identity, Latest: candidate.Identity}
	if !result.Changed || dry {
		return result, nil
	}
	if err := observed.commit(ctx, candidate, candidate); err != nil {
		return Result{}, err
	}
	return result, nil
}

func checkForward(latest, candidate Identity) error {
	if latest.SHA256 == "" {
		return nil
	}
	if candidate.Sequence < latest.Sequence {
		return fmt.Errorf("catalog rollback refused: candidate sequence %d is older than highest accepted sequence %d; use explicit local rollback", candidate.Sequence, latest.Sequence)
	}
	if candidate.Sequence == latest.Sequence && candidate.SHA256 != latest.SHA256 {
		return fmt.Errorf("catalog equivocation refused: sequence %d names a different digest", candidate.Sequence)
	}
	if candidate.Sequence > latest.Sequence && candidate.SHA256 == latest.SHA256 {
		return errors.New("a new catalog sequence must identify a new snapshot")
	}
	return nil
}

func Rollback(ctx context.Context, root, sha string, dry bool, trust publication.TrustRoot) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	observed, err := readStore(root, trust)
	if err != nil {
		return Result{}, err
	}
	if observed.active.SHA256 == "" {
		return Result{}, errors.New("no active catalog to roll back")
	}
	candidate, err := observed.readSnapshot(sha, trust)
	if err != nil {
		return Result{}, err
	}
	if candidate.Sequence > observed.latest.Sequence || candidate.Sequence == observed.latest.Sequence && candidate.SHA256 != observed.latest.SHA256 {
		return Result{}, errors.New("rollback snapshot exceeds or conflicts with the highest accepted publication")
	}
	result := Result{Changed: candidate.SHA256 != observed.active.SHA256, DryRun: dry,
		PreviousSHA256: observed.active.SHA256, Active: candidate.Identity, Latest: observed.latest.Identity}
	if !result.Changed || dry {
		return result, nil
	}
	if err := observed.commit(ctx, candidate, observed.latest); err != nil {
		return Result{}, err
	}
	return result, nil
}

func Read(root string, trust publication.TrustRoot) (Snapshot, error) {
	s, err := readStore(root, trust)
	if err != nil {
		return Snapshot{}, err
	}
	if s.active.SHA256 == "" {
		return Snapshot{}, errors.New("no verified local catalog; run temper catalog update first")
	}
	return s.active, nil
}

type Inspection struct {
	Active    Snapshot
	Latest    Identity
	Snapshots []Identity
}

func Inspect(root string, trust publication.TrustRoot) (Inspection, error) {
	s, err := readStore(root, trust)
	if err != nil {
		return Inspection{}, err
	}
	if s.active.SHA256 == "" {
		return Inspection{}, errors.New("no verified local catalog; run temper catalog update first")
	}
	identities, err := s.snapshots(trust)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Active: s.active, Latest: s.latest.Identity, Snapshots: identities}, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneArtifact(a publication.SignedArtifact) publication.SignedArtifact {
	return publication.SignedArtifact{Data: append([]byte(nil), a.Data...), Signature: append([]byte(nil), a.Signature...)}
}
