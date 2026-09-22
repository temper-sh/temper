package distribution

import (
	"path/filepath"

	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

// VerifyTree reads the exact files that Pages serves and verifies their complete
// signed join. It does not contact the declared locator or write any file.
func VerifyTree(root string, trust publication.TrustRoot) (Snapshot, error) {
	channelPath := filepath.Join(root, "channels", Channel)
	for _, path := range []string{root, filepath.Join(root, "channels"), channelPath, filepath.Join(root, "snapshots")} {
		if err := checkDirectory(path); err != nil {
			return Snapshot{}, err
		}
	}
	channel, err := readArtifact(channelPath, "channel.yaml", "channel.signature.yaml", catalogsource.MaxChannelBytes)
	if err != nil {
		return Snapshot{}, err
	}
	verified, err := verifyChannel(channel, trust)
	if err != nil {
		return Snapshot{}, err
	}
	path := filepath.Join(root, "snapshots", verified.Document.Catalog.SHA256)
	if err := checkDirectory(path); err != nil {
		return Snapshot{}, err
	}
	data, err := readArtifact(path, "catalog.json", "catalog.signature.yaml", catalogsource.MaxCatalogBytes)
	if err != nil {
		return Snapshot{}, err
	}
	return Verify(Publication{Channel: channel, Catalog: data}, trust)
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
