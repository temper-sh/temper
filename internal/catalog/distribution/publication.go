package distribution

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/temper-sh/temper/internal/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

// VerifyTree reads the exact files that the site serves and verifies the
// complete signed publication: the channel; for every platform, the snapshot
// folder, its signature and its digest; and, for this Temper's platform, the
// catalog itself. It does not contact the declared locator or write any file.
func VerifyTree(root string, trust publication.TrustRoot) (Snapshot, error) {
	return VerifyStaged(root, "", trust)
}

// VerifyStaged verifies a publication whose new files are staged apart from
// the tree: a staged channel or snapshot wins over the tree's.
func VerifyStaged(root, stage string, trust publication.TrustRoot) (Snapshot, error) {
	find := func(parts ...string) (string, error) {
		for _, base := range []string{stage, root} {
			if base == "" {
				continue
			}
			path := filepath.Join(append([]string{base}, parts...)...)
			if err := checkDirectory(path); err == nil {
				return path, nil
			} else if !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
		}
		return "", fmt.Errorf("publication has no %s", filepath.Join(parts...))
	}
	channelPath, err := find("channels", Channel)
	if err != nil {
		return Snapshot{}, err
	}
	channel, err := readArtifact(channelPath, "channel.yaml", "channel.signature.yaml", catalogsource.MaxChannelBytes)
	if err != nil {
		return Snapshot{}, err
	}
	verified, err := publication.VerifyChannel(Channel, channel.Data, channel.Signature, trust)
	if err != nil {
		return Snapshot{}, err
	}
	var own Snapshot
	platforms := make([]string, 0, len(verified.Document.Catalogs))
	for platform := range verified.Document.Catalogs {
		platforms = append(platforms, platform)
	}
	slices.Sort(platforms)
	for _, platform := range platforms {
		ref, err := verified.Document.Catalog(platform)
		if err != nil {
			return Snapshot{}, err
		}
		path, err := find("snapshots", ref.SHA256)
		if err != nil {
			return Snapshot{}, err
		}
		data, err := readArtifact(path, "catalog.json", "catalog.signature.yaml", catalogsource.MaxCatalogBytes)
		if err != nil {
			return Snapshot{}, err
		}
		if platform == catalog.Platform {
			if own, err = Verify(Publication{Channel: channel, Catalog: data}, trust); err != nil {
				return Snapshot{}, err
			}
			continue
		}
		// Another platform's catalog: this Temper checks only what it signs.
		if _, err := trust.Verify(data.Data, data.Signature); err != nil {
			return Snapshot{}, fmt.Errorf("%s catalog: %w", platform, err)
		}
		if digest(data.Data) != ref.SHA256 {
			return Snapshot{}, fmt.Errorf("%s catalog bytes do not match the signed channel digest", platform)
		}
	}
	if own.SHA256 == "" {
		return Snapshot{}, fmt.Errorf("%w: %s", publication.ErrNoPlatform, catalog.Platform)
	}
	return own, nil
}

func readArtifact(path, dataName, signatureName string, limit int64) (publication.SignedArtifact, error) {
	data, err := readFile(filepath.Join(path, dataName), limit)
	if err != nil {
		return publication.SignedArtifact{}, err
	}
	signature, err := readFile(filepath.Join(path, signatureName), catalogsource.MaxSignatureBytes)
	if err != nil {
		return publication.SignedArtifact{}, err
	}
	return publication.SignedArtifact{Data: data, Signature: signature}, nil
}
