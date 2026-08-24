// Package upstreamrelease implements the isolated release-artifact adapter.
// Catalog resolution is deliberately local: a reviewed target asset is copied
// into one exact lock unit, while installation owns the later network read.
package upstreamrelease

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/catalog"
)

const (
	adapterID = "upstream-release"
	method    = "release-artifact"
)

var (
	stableIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`)
	revisionPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._/+:-][a-z0-9]+)*$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Resolver performs no provider read. Its candidate is the exact release
// already reviewed into the catalog snapshot.
type Resolver struct{}

var _ adapter.CandidateResolver = (*Resolver)(nil)

func NewResolver() *Resolver { return &Resolver{} }

func Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		ID: adapterID, Method: method, Protocol: catalog.AdapterProtocolV1,
		EffectModel: "isolated",
		Targets:     []software.Target{{OS: "darwin", Arch: "arm64"}},
	}
}

func (r *Resolver) Descriptor() adapter.Descriptor { return Descriptor() }

func (r *Resolver) Candidates(ctx context.Context, request adapter.ResolveRequest) ([]software.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	artifact, err := validateResolveRequest(request)
	if err != nil {
		return nil, err
	}
	unitID := adapterID + ":" + request.Package
	lockedArtifact := software.Artifact{
		Locator: artifact.Locator, SHA256: artifact.SHA256, Size: artifact.Size,
		UnpackedSize: artifact.UnpackedSize, InstalledEntries: artifact.InstalledEntries, Format: artifact.Format, ArchiveRoot: artifact.ArchiveRoot,
	}
	return []software.Candidate{{
		RootUnit: unitID,
		Units: map[string]software.ResolvedUnit{unitID: {
			Scope: request.Package, NativeName: request.Recipe.Source.Name,
			Version: request.Recipe.Selection.Exact, Revision: request.Recipe.Source.Revision,
			Artifacts: []software.Artifact{lockedArtifact},
		}},
	}}, nil
}

func validateResolveRequest(request adapter.ResolveRequest) (catalog.ReleaseArtifact, error) {
	if !stableIDPattern.MatchString(request.Package) {
		return catalog.ReleaseArtifact{}, fmt.Errorf("release artifact package %q is not a lowercase stable id", request.Package)
	}
	if err := request.Target.Validate(); err != nil {
		return catalog.ReleaseArtifact{}, fmt.Errorf("release artifact target: %w", err)
	}
	if request.Target.OS != "darwin" || request.Target.Arch != "arm64" {
		return catalog.ReleaseArtifact{}, fmt.Errorf("upstream release adapter does not support target %s/%s", request.Target.OS, request.Target.Arch)
	}
	recipe := request.Recipe
	if recipe.Method != method || recipe.Source.Kind != "release-archive" {
		return catalog.ReleaseArtifact{}, errors.New("upstream release adapter requires a release-artifact release-archive recipe")
	}
	if recipe.Selection.Policy != "exact" || strings.TrimSpace(recipe.Selection.Exact) == "" {
		return catalog.ReleaseArtifact{}, errors.New("release archive recipe requires one exact catalog-reviewed version")
	}
	if !stableIDPattern.MatchString(recipe.Source.Name) || !revisionPattern.MatchString(recipe.Source.Revision) || strings.TrimSpace(recipe.Source.Repository) == "" {
		return catalog.ReleaseArtifact{}, errors.New("release archive source identity is incomplete")
	}
	artifact, err := recipe.Source.ReleaseArtifactFor(request.Target)
	if err != nil {
		return catalog.ReleaseArtifact{}, fmt.Errorf("select release artifact: %w", err)
	}
	if !validHTTPSLocator(artifact.Locator) {
		return catalog.ReleaseArtifact{}, errors.New("release artifact locator must be an absolute https URL without credentials or fragment")
	}
	if !sha256Pattern.MatchString(artifact.SHA256) || artifact.Size <= 0 || artifact.UnpackedSize <= 0 || artifact.InstalledEntries <= 0 {
		return catalog.ReleaseArtifact{}, errors.New("release artifact requires exact positive sizes and a lowercase SHA-256")
	}
	if artifact.Format != "tar.gz" || !safeArchivePath(artifact.ArchiveRoot, true) {
		return catalog.ReleaseArtifact{}, errors.New("release artifact must be a tar.gz with a safe archive root")
	}
	return artifact, nil
}

func validHTTPSLocator(locator string) bool {
	parsed, err := url.Parse(locator)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}
