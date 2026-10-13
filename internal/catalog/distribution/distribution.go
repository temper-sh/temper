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

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

const Channel = "stable"

// ErrNoCatalog means the local store is valid but has no accepted snapshot.
// A malformed or tampered store returns its specific error instead.
var ErrNoCatalog = errors.New("no verified local catalog; run temper catalog update first")

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
	// Document holds the records this Temper can use; Unavailable says why
	// the others are not.
	Document     catalog.Document
	Unavailable  []catalog.Unavailable
	ChannelKeyID string
	CatalogKeyID string
	publication  Publication
}

// Source reads the signed channel and one catalog snapshot.
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

// Verify binds exact catalog bytes to this platform's entry in a signed
// channel. It does not read a URL, filesystem, upstream release, or machine
// state.
func Verify(p Publication, trust publication.TrustRoot) (Snapshot, error) {
	channel, ref, err := verifyChannel(p.Channel, trust)
	if err != nil {
		return Snapshot{}, err
	}
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
	view, err := catalog.Read(p.Catalog.Data, buildinfo.Version)
	if err != nil {
		return Snapshot{}, err
	}
	d := view.Document
	if d.Schema != ref.Schema || d.Platform != catalog.Platform {
		return Snapshot{}, errors.New("catalog schema or platform does not match the signed channel")
	}
	return Snapshot{
		Identity: Identity{SHA256: ref.SHA256, Sequence: channel.Document.Sequence},
		Document: d, Unavailable: view.Unavailable, ChannelKeyID: channel.KeyID, CatalogKeyID: key,
		publication: Publication{
			Channel: cloneArtifact(p.Channel), Catalog: cloneArtifact(p.Catalog),
		},
	}, nil
}

// verifyChannel checks a channel's signature and returns this platform's
// entry; other platforms' entries stay unparsed.
func verifyChannel(artifact publication.SignedArtifact, trust publication.TrustRoot) (publication.VerifiedChannel, publication.CatalogReference, error) {
	if len(artifact.Data) > catalogsource.MaxChannelBytes || len(artifact.Signature) > catalogsource.MaxSignatureBytes {
		return publication.VerifiedChannel{}, publication.CatalogReference{}, errors.New("catalog channel exceeds its size limit")
	}
	c, err := publication.VerifyChannel(Channel, artifact.Data, artifact.Signature, trust)
	if err != nil {
		return c, publication.CatalogReference{}, err
	}
	ref, err := c.Document.Catalog(catalog.Platform)
	if err != nil {
		return c, publication.CatalogReference{}, err
	}
	return c, ref, nil
}

// channelIdentity is what a verified channel names for this platform.
func channelIdentity(artifact publication.SignedArtifact, trust publication.TrustRoot) (Identity, error) {
	c, ref, err := verifyChannel(artifact, trust)
	if err != nil {
		return Identity{}, err
	}
	return Identity{SHA256: ref.SHA256, Sequence: c.Document.Sequence}, nil
}

func Update(ctx context.Context, root string, dry bool, trust publication.TrustRoot, source Source) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	observed, err := readStore(root, trust)
	if err != nil {
		return Result{}, err
	}
	channel, named, err := fetchChannel(ctx, observed.floor, trust, source)
	if err != nil {
		return Result{}, err
	}
	result := Result{DryRun: dry, PreviousSHA256: observed.active.SHA256, Active: observed.active.Identity, Latest: named}
	if named.SHA256 == observed.active.SHA256 {
		// Another platform changed: nothing to download, but the newer
		// sequence becomes the floor.
		if named.Sequence > observed.floor.Sequence && !dry {
			if err := observed.commit(ctx, observed.active, observed.latest, &channel); err != nil {
				return Result{}, err
			}
		}
		return result, nil
	}
	candidate, err := fetchCatalog(ctx, channel, named, trust, source)
	if err != nil {
		return Result{}, err
	}
	result.Changed, result.Active = true, candidate.Identity
	if dry {
		return result, nil
	}
	if err := observed.commit(ctx, candidate, candidate, &channel); err != nil {
		return Result{}, err
	}
	return result, nil
}

// Preview verifies the current signed publication and the local highest
// accepted sequence without creating or changing the catalog store. A caller
// can prepare an initial selection before deciding to persist anything.
func Preview(ctx context.Context, root string, trust publication.TrustRoot, source Source) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	observed, err := readStore(root, trust)
	if err != nil {
		return Snapshot{}, err
	}
	channel, named, err := fetchChannel(ctx, observed.floor, trust, source)
	if err != nil {
		return Snapshot{}, err
	}
	return fetchCatalog(ctx, channel, named, trust, source)
}

func fetchChannel(ctx context.Context, floor Identity, trust publication.TrustRoot, source Source) (publication.SignedArtifact, Identity, error) {
	if source == nil {
		return publication.SignedArtifact{}, Identity{}, errors.New("catalog source is required")
	}
	channel, err := source.Channel(ctx, Channel)
	if err != nil {
		return publication.SignedArtifact{}, Identity{}, fmt.Errorf("read catalog channel: %w", err)
	}
	named, err := channelIdentity(channel, trust)
	if err != nil {
		return publication.SignedArtifact{}, Identity{}, err
	}
	if err := checkForward(floor, named); err != nil {
		return publication.SignedArtifact{}, Identity{}, err
	}
	return channel, named, nil
}

func fetchCatalog(ctx context.Context, channel publication.SignedArtifact, named Identity, trust publication.TrustRoot, source Source) (Snapshot, error) {
	_, ref, err := verifyChannel(channel, trust)
	if err != nil {
		return Snapshot{}, err
	}
	data, err := source.CatalogJSON(ctx, ref.Locator)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read catalog snapshot: %w", err)
	}
	candidate, err := Verify(Publication{Channel: channel, Catalog: data}, trust)
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return candidate, nil
}

// checkForward accepts a channel at or after the floor. A higher sequence
// may name the same catalog: another platform changed.
func checkForward(floor, candidate Identity) error {
	if floor.SHA256 == "" {
		return nil
	}
	if candidate.Sequence < floor.Sequence {
		return fmt.Errorf("catalog rollback refused: candidate sequence %d is older than highest accepted sequence %d; use explicit local rollback", candidate.Sequence, floor.Sequence)
	}
	if candidate.Sequence == floor.Sequence && candidate.SHA256 != floor.SHA256 {
		return fmt.Errorf("catalog equivocation refused: sequence %d names a different digest", candidate.Sequence)
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
	if candidate.Sequence > observed.floor.Sequence || candidate.Sequence == observed.floor.Sequence && candidate.SHA256 != observed.floor.SHA256 {
		return Result{}, errors.New("rollback snapshot exceeds or conflicts with the highest accepted publication")
	}
	result := Result{Changed: candidate.SHA256 != observed.active.SHA256, DryRun: dry,
		PreviousSHA256: observed.active.SHA256, Active: candidate.Identity, Latest: observed.floor}
	if !result.Changed || dry {
		return result, nil
	}
	if err := observed.commit(ctx, candidate, observed.latest, nil); err != nil {
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
		return Snapshot{}, ErrNoCatalog
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
		return Inspection{}, ErrNoCatalog
	}
	identities, err := s.snapshots(trust)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Active: s.active, Latest: s.floor, Snapshots: identities}, nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneArtifact(a publication.SignedArtifact) publication.SignedArtifact {
	return publication.SignedArtifact{Data: append([]byte(nil), a.Data...), Signature: append([]byte(nil), a.Signature...)}
}
