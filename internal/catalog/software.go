package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
)

// Software choices: the catalog's recorded (tested) releases, or the newest
// allowed upstream releases.
const (
	SoftwareStable = "stable"
	SoftwareLatest = "latest"
)

func (s Supply) validateSource() error {
	if s.Python != nil {
		_, _, err := s.pythonInputs()
		return err
	}
	if !idPattern.MatchString(s.Package) || s.Source == nil {
		return errors.New("supply requires package, target and source; installer selection and units are derived")
	}
	if err := s.Source.Validate(); err != nil {
		return err
	}
	if s.Release == nil {
		return nil
	} // Moving sources are resolved before compilation.
	r := s.Release
	if _, err := upstreamrelease.CompareVersions(r.Version, r.Version); err != nil {
		return err
	}
	if !revisionPattern.MatchString(r.Revision) {
		return errors.New("resolved release needs its exact commit")
	}
	a := r.Artifact
	locator, err := url.Parse(a.Locator)
	if err != nil || locator.Scheme != "https" || locator.Host == "" || locator.User != nil || locator.Fragment != "" || !hashPattern.MatchString(a.SHA256) || a.Size <= 0 || a.UnpackedSize <= 0 || a.InstalledEntries <= 0 || a.Format != "tar.gz" || (a.ArchiveRoot != "." && !safePath(a.ArchiveRoot)) {
		return errors.New("resolved release needs an exact HTTPS tar.gz, checksum, sizes, entry count and safe root")
	}
	// The recorded download is tied to its source: a non-GitHub source needs
	// a new field, so an older Temper hides it instead of trusting any URL.
	if a.Locator != s.Source.ReleaseURL(r.Version) {
		return fmt.Errorf("release %s must be downloaded from %s", r.Version, s.Source.ReleaseURL(r.Version))
	}
	return nil
}

func (s Supply) installerInputs() (softwarelock.Selection, map[string]softwarelock.Unit, error) {
	if s.Python != nil {
		return s.pythonInputs()
	}
	if s.Release == nil {
		return softwarelock.Selection{}, nil, fmt.Errorf("software %q is unresolved; compile with --software latest", s.Package)
	}
	id := "upstream-release:" + s.Package
	selection := softwarelock.Selection{Provenance: softwarelock.ProvenanceExecution, Method: "release-artifact", Adapter: "upstream-release", RecipeRevision: "release-archive/v1", RootUnit: id}
	units := map[string]softwarelock.Unit{id: {Adapter: "upstream-release", Scope: s.Package, NativeName: s.Package, Version: s.Release.Version, Revision: s.Release.Revision, Dependencies: []string{}, Artifacts: []software.Artifact{s.Release.Artifact}}}
	return selection, units, nil
}

// LatestReleases discovers each upstream source's newest release at most once
// per command run, so every preset sees the same router release.
type LatestReleases struct {
	Reader upstreamrelease.ArtifactReader
	found  map[upstreamrelease.GitHubSource]latestResult
}

type latestResult struct {
	release upstreamrelease.Release
	err     error
}

func (l *LatestReleases) discover(ctx context.Context, source upstreamrelease.GitHubSource) (upstreamrelease.Release, error) {
	if l == nil {
		return upstreamrelease.Release{}, errors.New("no upstream reader")
	}
	if result, ok := l.found[source]; ok {
		return result.release, result.err
	}
	if l.Reader == nil {
		return upstreamrelease.Release{}, errors.New("no upstream reader")
	}
	release, err := upstreamrelease.Discover(ctx, l.Reader, source, SoftwareLatest)
	if err != nil && ctx.Err() != nil {
		return release, err // Cancellation is not a result to remember.
	}
	if l.found == nil {
		l.found = map[upstreamrelease.GitHubSource]latestResult{}
	}
	l.found[source] = latestResult{release, err}
	return release, err
}

// Resolution reports the release one software item uses. Reason explains a
// fallback from latest to the catalog's stable release.
type Resolution struct {
	Package   string `json:"package"`
	Requested string `json:"requested"`
	Used      string `json:"used"`
	Reason    string `json:"reason,omitempty"`
}

// ResolveSoftware reads only the selected preset's sources. Stable keeps the
// catalog's recorded releases offline. Latest reads the newest upstream
// releases; one the catalog excludes, or a failed lookup, is an error unless
// fallback keeps that item's stable release and reports why. Tools never fall
// back, so a "latest" test never quietly tests the stable release.
func ResolveSoftware(ctx context.Context, d Document, id string, choice string, latest *LatestReleases, fallback bool) (Document, []Resolution, error) {
	if err := d.validateRecords(); err != nil {
		return Document{}, nil, err
	}
	if choice != SoftwareStable && choice != SoftwareLatest {
		return Document{}, nil, errors.New("software choice must be stable or latest")
	}
	p, ok := d.Presets[id]
	if !ok {
		return Document{}, nil, fmt.Errorf("unknown preset %q", id)
	}
	if choice == SoftwareStable {
		return d, nil, nil
	}
	d = canonicalDocument(d)
	var resolutions []Resolution
	resolve := func(supply Supply, rules ...[]VersionExclusion) (Supply, error) {
		if supply.Python != nil {
			return Supply{}, fmt.Errorf("software %q is an exact Python closure; use stable software and resolve a new closure explicitly", supply.Package)
		}
		resolution := Resolution{Package: supply.Package, Requested: SoftwareLatest}
		release, err := latest.discover(ctx, *supply.Source)
		if err == nil {
			err = allowedRelease(&release, rules...)
		}
		if err != nil {
			if !fallback || supply.Release == nil || ctx.Err() != nil {
				return Supply{}, fmt.Errorf("latest %s: %w", supply.Package, err)
			}
			resolution.Used, resolution.Reason = supply.Release.Version, err.Error()
			resolutions = append(resolutions, resolution)
			return supply, nil
		}
		resolution.Used = release.Version
		resolutions = append(resolutions, resolution)
		supply.Release = &release
		return supply, nil
	}
	router := d.Runtime.Router
	supply, err := resolve(router.Supply, router.ExcludeVersions)
	if err != nil {
		return Document{}, nil, err
	}
	router.Supply = supply
	d.Runtime.Router = router
	e := d.Engines[p.Engine]
	e.Supply, err = resolve(e.Supply, e.ExcludeVersions, p.ExcludeEngineVersions)
	if err != nil {
		return Document{}, nil, err
	}
	d.Engines[p.Engine] = e
	return d, resolutions, nil
}
