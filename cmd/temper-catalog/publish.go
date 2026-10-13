package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
	"github.com/temper-sh/temper/internal/software/catalogsource"
)

// runPublish turns one authored catalog source into a signed publication:
// the snapshot, then the next channel with only this platform's entry
// replaced.
func runPublish(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer, tool catalogsigning.Tool) int {
	flags := flag.NewFlagSet("temper-catalog publish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	sourcePath := flags.String("source", "", "authored catalog source for one platform")
	root := flags.String("root", "", "publication tree (catalog/v1)")
	dry := flags.Bool("dry-run", false, "check and report without writing into the tree")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *sourcePath == "" || *root == "" {
		fmt.Fprintln(stderr, "usage: temper-catalog publish --source FILE --root DIRECTORY [--dry-run]  (seed on stdin)")
		return 2
	}
	result, err := publish(ctx, *sourcePath, *root, *dry, stdin, tool, catalogsource.ProductionSnapshotRoot, time.Now().UTC())
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog publish: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "RESULT catalog-publish %s platform=%s sequence=%d sha256=%s presets=%d\n", result.status, result.platform, result.sequence, result.sha256, result.presets)
	return 0
}

type published struct {
	status, platform, sha256 string
	sequence                 uint64
	presets                  int
}

func publish(ctx context.Context, sourcePath, root string, dry bool, stdin io.Reader, tool catalogsigning.Tool, snapshotRoot string, now time.Time) (published, error) {
	raw, _, err := readRegularFile(sourcePath, catalogsource.MaxCatalogBytes, "catalog source")
	if err != nil {
		return published{}, err
	}
	d, err := catalog.Parse(raw)
	if err != nil {
		return published{}, fmt.Errorf("catalog source: %w", err)
	}
	sum := sha256.Sum256(raw)
	result := published{platform: d.Platform, sha256: hex.EncodeToString(sum[:]), presets: len(d.Presets)}

	next := publication.Channel{Schema: publication.CurrentChannelSchema, Channel: distribution.Channel}
	channelDir := filepath.Join(root, "channels", distribution.Channel)
	previousDate := ""
	if channel, err := readArtifact(channelDir, "channel.yaml", "channel.signature.yaml", catalogsource.MaxChannelBytes); err == nil {
		verified, err := publication.VerifyChannel(distribution.Channel, channel.Data, channel.Signature, tool.Trust())
		if err != nil {
			return published{}, fmt.Errorf("current channel: %w", err)
		}
		next = verified.Document
		if ref, err := next.Catalog(d.Platform); err == nil {
			if ref.SHA256 == result.sha256 {
				result.status, result.sequence = "unchanged", next.Sequence
				return result, nil
			}
			if previousDate, err = snapshotDate(filepath.Join(root, "snapshots", ref.SHA256, "catalog.json")); err != nil {
				return published{}, fmt.Errorf("current %s catalog: %w", d.Platform, err)
			}
		} else if !errors.Is(err, publication.ErrNoPlatform) {
			return published{}, fmt.Errorf("current channel: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return published{}, err
	}
	if today := now.UTC().Format(time.DateOnly); d.Date < previousDate || d.Date > today {
		return published{}, fmt.Errorf("catalog date %s must be between the current catalog's %s and today %s (UTC)", d.Date, previousDate, today)
	}
	next.Sequence++
	next, err = next.WithCatalog(d.Platform, publication.CatalogReference{Schema: catalog.Schema, SHA256: result.sha256, Locator: snapshotRoot + result.sha256 + "/"})
	if err != nil {
		return published{}, err
	}
	channelData, err := publication.MarshalChannel(next)
	if err != nil {
		return published{}, err
	}
	result.sequence = next.Sequence

	seedInput, err := io.ReadAll(io.LimitReader(stdin, catalogsigning.MaxSeedInputBytes+1))
	if err != nil {
		return published{}, errors.New("read catalog signing seed from stdin")
	}
	defer clear(seedInput)
	seed, err := catalogsigning.ParseSeed(seedInput)
	if err != nil {
		return published{}, err
	}
	defer clear(seed)
	catalogSignature, err := tool.Sign(catalogsigning.KindCatalog, "", raw, seed)
	if err != nil {
		return published{}, err
	}
	channelSignature, err := tool.Sign(catalogsigning.KindChannel, distribution.Channel, channelData, seed)
	if err != nil {
		return published{}, err
	}

	// Stage outside the tree for a dry run; inside it otherwise, so the
	// commit is a rename.
	base := ""
	if !dry {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return published{}, err
		}
		base = root
	}
	stage, err := os.MkdirTemp(base, ".temper-catalog-publish-")
	if err != nil {
		return published{}, err
	}
	defer os.RemoveAll(stage)
	stagedSnapshot := filepath.Join(stage, "snapshots", result.sha256)
	stagedChannel := filepath.Join(stage, "channels", distribution.Channel)
	for dir, files := range map[string]map[string][]byte{
		stagedSnapshot: {"catalog.json": raw, "catalog.signature.yaml": catalogSignature},
		stagedChannel:  {"channel.yaml": channelData, "channel.signature.yaml": channelSignature},
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return published{}, err
		}
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				return published{}, err
			}
		}
	}
	verified, err := distribution.VerifyStaged(root, stage, tool.Trust())
	if err != nil {
		return published{}, fmt.Errorf("staged publication: %w", err)
	}
	if d.Platform == catalog.Platform && verified.SHA256 != result.sha256 {
		return published{}, errors.New("staged publication does not name the source")
	}
	if dry {
		result.status = "would-publish"
		return result, nil
	}
	if err := ctx.Err(); err != nil {
		return published{}, err
	}
	// The snapshot first: a channel never names a missing snapshot.
	target := filepath.Join(root, "snapshots", result.sha256)
	if existing, err := os.ReadFile(filepath.Join(target, "catalog.json")); err == nil {
		if !bytes.Equal(existing, raw) {
			return published{}, fmt.Errorf("snapshot %s exists with different bytes", result.sha256)
		}
	} else if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return published{}, err
		}
		if err := os.Rename(stagedSnapshot, target); err != nil {
			return published{}, err
		}
	} else {
		return published{}, err
	}
	// The channel pair last, swapped in as one folder.
	if err := os.MkdirAll(filepath.Dir(channelDir), 0o755); err != nil {
		return published{}, err
	}
	aside := filepath.Join(stage, "previous-channel")
	if err := os.Rename(channelDir, aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return published{}, err
	}
	if err := os.Rename(stagedChannel, channelDir); err != nil {
		return published{}, errors.Join(err, os.Rename(aside, channelDir))
	}
	result.status = "published"
	return result, nil
}

// snapshotDate reads only the date of a published catalog.
func snapshotDate(path string) (string, error) {
	raw, _, err := readRegularFile(path, catalogsource.MaxCatalogBytes, "published catalog")
	if err != nil {
		return "", err
	}
	var head struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", err
	}
	return head.Date, nil
}

func readArtifact(dir, dataName, signatureName string, limit int64) (publication.SignedArtifact, error) {
	data, _, err := readRegularFile(filepath.Join(dir, dataName), limit, dataName)
	if err != nil {
		return publication.SignedArtifact{}, err
	}
	signature, _, err := readRegularFile(filepath.Join(dir, signatureName), catalogsource.MaxSignatureBytes, signatureName)
	if err != nil {
		return publication.SignedArtifact{}, err
	}
	return publication.SignedArtifact{Data: data, Signature: signature}, nil
}
