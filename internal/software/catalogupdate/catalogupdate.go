// Package catalogupdate orchestrates authenticated catalog reads, pure
// capability and rollback checks, and one active-pointer commit.
package catalogupdate

import (
	"context"
	"errors"
	"fmt"

	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogstore"
)

type SignedArtifact struct {
	Data      []byte
	Signature []byte
}

// Source is the read-only catalog transport boundary. A signed locator is
// opaque input to Catalog; it is never executed or interpreted here.
type Source interface {
	Channel(context.Context, string) (SignedArtifact, error)
	Catalog(context.Context, string) (SignedArtifact, error)
}

// CapabilityValidator proves that the running binary implements every
// adapter contract and target binding declared by a catalog.
type CapabilityValidator interface {
	ValidateCatalog(catalog.Document) error
}

type Options struct {
	Root    string
	Channel string
	DryRun  bool
}

type Result struct {
	Changed      bool
	DryRun       bool
	Channel      string
	Sequence     uint64
	SHA256       string
	ChannelKeyID string
	CatalogKeyID string
}

func Run(ctx context.Context, options Options, trust publication.TrustRoot, source Source, capabilities CapabilityValidator) (Result, error) {
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Result{}, err
	}
	if err := publication.ValidateChannelName(options.Channel); err != nil {
		return Result{}, err
	}
	if source == nil {
		return Result{}, errors.New("catalog update source is required")
	}
	if capabilities == nil {
		return Result{}, errors.New("catalog capability validator is required")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	channelArtifact, err := source.Channel(ctx, options.Channel)
	if err != nil {
		return Result{}, fmt.Errorf("read catalog channel %q: %w", options.Channel, err)
	}
	verifiedChannel, err := publication.VerifyChannel(options.Channel, channelArtifact.Data, channelArtifact.Signature, trust)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	reference := verifiedChannel.Document.Catalog
	catalogArtifact, err := source.Catalog(ctx, reference.Locator)
	if err != nil {
		return Result{}, fmt.Errorf("read software catalog %q: %w", reference.Locator, err)
	}
	verifiedCatalog, err := publication.VerifyCatalog(reference, catalogArtifact.Data, catalogArtifact.Signature, trust)
	if err != nil {
		return Result{}, err
	}
	if err := capabilities.ValidateCatalog(verifiedCatalog.Snapshot.Document); err != nil {
		return Result{}, fmt.Errorf("software catalog is unsupported by this binary: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	active, err := catalogstore.Read(root)
	if err != nil {
		return Result{}, err
	}
	if active.Exists() {
		if _, err := trust.Verify(active.CatalogData, active.SignatureData); err != nil {
			return Result{}, fmt.Errorf("verify active software catalog signature: %w", err)
		}
		activeSequence := active.Catalog.Document.Sequence
		if reference.Sequence < activeSequence {
			return Result{}, fmt.Errorf("software catalog rollback refused: candidate sequence %d is older than active sequence %d", reference.Sequence, activeSequence)
		}
		if reference.Sequence == activeSequence && reference.SHA256 != active.Catalog.SHA256 {
			return Result{}, fmt.Errorf("software catalog equivocation refused: sequence %d has active digest %q and candidate digest %q", reference.Sequence, active.Catalog.SHA256, reference.SHA256)
		}
	}

	result := Result{
		DryRun:       options.DryRun,
		Channel:      options.Channel,
		Sequence:     reference.Sequence,
		SHA256:       reference.SHA256,
		ChannelKeyID: verifiedChannel.KeyID,
		CatalogKeyID: verifiedCatalog.KeyID,
	}
	if active.Exists() && active.Catalog.SHA256 == reference.SHA256 {
		return result, nil
	}
	result.Changed = true
	if options.DryRun {
		return result, nil
	}
	if err := active.Commit(ctx, catalogstore.Publication{
		CatalogData:   catalogArtifact.Data,
		SignatureData: catalogArtifact.Signature,
		Digest:        reference.SHA256,
	}); err != nil {
		return Result{}, err
	}
	return result, nil
}
