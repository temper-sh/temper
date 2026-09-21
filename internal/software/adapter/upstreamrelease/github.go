package upstreamrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/url"
	"regexp"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	softwarearchive "github.com/temper-sh/temper/internal/software/archive"
)

// GitHubSource selects a release asset for one catalog target. {version} is
// the full tag; {number} removes its b/v prefix. No shell expansion is involved.
type GitHubSource struct {
	Repository  string `yaml:"repository" json:"repository"`
	Asset       string `yaml:"asset" json:"asset"`
	ArchiveRoot string `yaml:"archive_root" json:"archive_root"`
}

type Release struct {
	Version  string            `yaml:"version" json:"version"`
	Revision string            `yaml:"revision" json:"revision"`
	Artifact software.Artifact `yaml:"artifact" json:"artifact"`
}

var githubRepo = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
var buildTag = regexp.MustCompile(`^([bv])([0-9]+)$`)
var commitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

func (s GitHubSource) Validate() error {
	if !githubRepo.MatchString(s.Repository) || strings.Contains(s.Repository, "..") {
		return errors.New("release source needs a GitHub owner/repository")
	}
	asset, root := expandRelease(s.Asset, "b1"), expandRelease(s.ArchiveRoot, "b1")
	if strings.ContainsAny(asset+root, "{}") || strings.ContainsAny(asset, "/\\") || !strings.HasSuffix(asset, ".tar.gz") || !safeArchivePath(root, true) {
		return errors.New("release source needs a tar.gz asset name and safe archive root; supported variables are {version} and {number}")
	}
	return nil
}

// CompareVersions orders the numbered release tags used by llama.cpp and
// llama-swap. Different tag families and unknown formats are not guessed.
func CompareVersions(left, right string) (int, error) {
	l, r := buildTag.FindStringSubmatch(left), buildTag.FindStringSubmatch(right)
	if l == nil || r == nil || l[1] != r[1] {
		return 0, fmt.Errorf("cannot compare release versions %q and %q: expected matching b<number> or v<number> tags", left, right)
	}
	a, _ := new(big.Int).SetString(l[2], 10)
	b, _ := new(big.Int).SetString(r[2], 10)
	return a.Cmp(b), nil
}

// Discover reads upstream metadata and verifies the selected archive in memory
// as a stream. It neither installs software nor writes a cache or catalog.
// See https://docs.github.com/en/rest/releases/releases#get-the-latest-release.
func Discover(ctx context.Context, reader ArtifactReader, source GitHubSource, requested string) (Release, error) {
	if err := source.Validate(); err != nil {
		return Release{}, err
	}
	if reader == nil {
		return Release{}, errors.New("release reader is required")
	}
	endpoint := "https://api.github.com/repos/" + source.Repository
	path := "/releases/latest"
	if requested != "latest" {
		if _, err := CompareVersions(requested, requested); err != nil {
			return Release{}, err
		}
		path = "/releases/tags/" + url.PathEscape(requested)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := readGitHub(ctx, reader, endpoint+path, &release); err != nil {
		return Release{}, err
	}
	if release.Draft || release.Prerelease || (requested != "latest" && release.Tag != requested) {
		return Release{}, errors.New("upstream did not return the requested stable release")
	}
	if _, err := CompareVersions(release.Tag, release.Tag); err != nil {
		return Release{}, err
	}
	name := expandRelease(source.Asset, release.Tag)
	var artifact software.Artifact
	found := false
	for _, asset := range release.Assets {
		if asset.Name != name {
			continue
		}
		if found {
			return Release{}, fmt.Errorf("release repeats asset %q", name)
		}
		found = true
		wantURL := "https://github.com/" + source.Repository + "/releases/download/" + url.PathEscape(release.Tag) + "/" + name
		hash := strings.TrimPrefix(asset.Digest, "sha256:")
		if asset.URL != wantURL || asset.Size <= 0 || asset.Size > 512<<20 || !strings.HasPrefix(asset.Digest, "sha256:") || !sha256Pattern.MatchString(hash) {
			return Release{}, errors.New("release asset needs its exact GitHub download URL, SHA-256 and size (at most 512 MiB)")
		}
		artifact = software.Artifact{Locator: asset.URL, SHA256: hash, Size: asset.Size, Format: "tar.gz", ArchiveRoot: expandRelease(source.ArchiveRoot, release.Tag)}
	}
	if !found {
		return Release{}, fmt.Errorf("release %s has no target asset %q", release.Tag, name)
	}
	var ref struct {
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if err := readGitHub(ctx, reader, endpoint+"/git/ref/tags/"+url.PathEscape(release.Tag), &ref); err != nil {
		return Release{}, err
	}
	for n := 0; ref.Object.Type == "tag" && n < 8; n++ {
		if !commitSHA.MatchString(ref.Object.SHA) {
			return Release{}, errors.New("invalid annotated tag identity")
		}
		if err := readGitHub(ctx, reader, endpoint+"/git/tags/"+ref.Object.SHA, &ref); err != nil {
			return Release{}, err
		}
	}
	if ref.Object.Type != "commit" || !commitSHA.MatchString(ref.Object.SHA) {
		return Release{}, errors.New("release tag did not resolve to an exact commit")
	}
	body, err := reader.Open(ctx, artifact.Locator)
	if err != nil {
		return Release{}, err
	}
	defer body.Close()
	limited := &io.LimitedReader{R: body, N: artifact.Size + 1}
	hash := sha256.New()
	stream := io.TeeReader(limited, hash)
	entries, err := softwarearchive.InspectTarGzStream(ctx, stream, softwarearchive.TarGzSpec{Root: artifact.ArchiveRoot, MaxEntries: 100000, MaxUnpackedBytes: 2 << 30, Label: "upstream release"})
	if err != nil {
		return Release{}, err
	}
	if _, err := io.Copy(io.Discard, stream); err != nil {
		return Release{}, err
	}
	if limited.N != 1 || hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		return Release{}, errors.New("upstream release archive differs from its size or SHA-256")
	}
	artifact.InstalledEntries = len(entries)
	for _, entry := range entries {
		artifact.UnpackedSize += entry.Size
	}
	if artifact.UnpackedSize == 0 {
		return Release{}, errors.New("release archive contains no file content")
	}
	return Release{Version: release.Tag, Revision: ref.Object.SHA, Artifact: artifact}, nil
}

func expandRelease(pattern, tag string) string {
	return strings.NewReplacer("{version}", tag, "{number}", strings.TrimLeft(tag, "bv")).Replace(pattern)
}

func readGitHub(ctx context.Context, reader ArtifactReader, locator string, into any) error {
	body, err := reader.Open(ctx, locator)
	if err != nil {
		return err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, (4<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 4<<20 {
		return errors.New("release metadata exceeds 4 MiB")
	}
	if err := json.Unmarshal(data, into); err != nil {
		return fmt.Errorf("decode upstream release metadata: %w", err)
	}
	return nil
}
