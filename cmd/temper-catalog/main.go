// Command temper-catalog is the release-side publisher and verifier for
// Temper's catalog publications. It is deliberately separate from
// the end-user temper binary and never discovers private key material.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(arguments) > 0 && arguments[0] == "availability" {
		return runAvailability(arguments[1:], stdout, stderr)
	}
	if len(arguments) > 0 && arguments[0] == "verify-publication" {
		trust, err := catalogtrust.Production()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return runVerifyPublication(arguments[1:], stdout, stderr, trust)
	}
	tool, err := newProductionTool()
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog: construct release tool: %v\n", err)
		return 1
	}
	return runWithTool(ctx, arguments, stdin, stdout, stderr, tool)
}

func runVerifyPublication(arguments []string, stdout, stderr io.Writer, trust publication.TrustRoot) int {
	flags := flag.NewFlagSet("temper-catalog verify-publication", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "catalog publication tree served by Pages")
	source := flags.String("source", "", "require the publication to match this authored catalog exactly")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" {
		fmt.Fprintln(stderr, "usage: temper-catalog verify-publication --root DIRECTORY [--source CATALOG]")
		return 2
	}
	snapshot, err := distribution.VerifyTree(*root, trust)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog verify-publication: %v\n", err)
		return 1
	}
	if *source != "" {
		raw, _, err := readRegularFile(*source, catalogsource.MaxCatalogBytes, "authored catalog")
		if err != nil {
			fmt.Fprintf(stderr, "temper-catalog verify-publication: %v\n", err)
			return 1
		}
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != snapshot.SHA256 {
			fmt.Fprintln(stderr, "temper-catalog verify-publication: signed snapshot differs from the authored catalog; prepare a new signed publication")
			return 1
		}
	}
	fmt.Fprintf(stdout, "The %s channel publication is verified: sequence %d, %d presets (sha256 %s).\n", distribution.Channel, snapshot.Sequence, len(snapshot.Document.Presets), snapshot.SHA256)
	return 0
}

// runAvailability reports which presets a Temper version can use from one
// catalog and why the others are unavailable. Release CI runs it for every
// supported version against the current publication.
func runAvailability(arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper-catalog availability", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("catalog", "", "catalog file to read")
	version := flags.String("temper-version", buildinfo.DevVersion, "Temper version to evaluate minimum_temper for; default: a development build")
	jsonOutput := flags.Bool("json", false, "print JSON")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *path == "" {
		fmt.Fprintln(stderr, "usage: temper-catalog availability --catalog FILE [--temper-version VERSION] [--json]")
		return 2
	}
	raw, _, err := readRegularFile(*path, catalogsource.MaxCatalogBytes, "catalog")
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog availability: %v\n", err)
		return 1
	}
	view, err := catalog.Read(raw, *version)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog availability: %v\n", err)
		return 1
	}
	available := make([]string, 0, len(view.Document.Presets))
	for id := range view.Document.Presets {
		available = append(available, id)
	}
	slices.Sort(available)
	unavailable := view.Unavailable
	if unavailable == nil {
		unavailable = []catalog.Unavailable{}
	}
	if *jsonOutput {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(map[string]any{"schema": "temper-catalog-availability/v1", "temper_version": *version, "available": available, "unavailable": unavailable}); err != nil {
			fmt.Fprintf(stderr, "temper-catalog availability: %v\n", err)
			return 1
		}
		return 0
	}
	presets := len(available)
	for _, u := range unavailable {
		if u.Kind == "preset" {
			presets++
		}
	}
	fmt.Fprintf(stdout, "Temper %s can use %d of %d presets.\n", *version, len(available), presets)
	for _, id := range available {
		fmt.Fprintf(stdout, "  available: %s\n", id)
	}
	for _, u := range unavailable {
		fmt.Fprintf(stdout, "  unavailable %s %s: %s\n", u.Kind, u.ID, u.Reason)
	}
	return 0
}

func newProductionTool() (catalogsigning.Tool, error) {
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalogsigning.Tool{}, err
	}
	return catalogsigning.New(catalogtrust.ProductionKeyID, trust)
}

func runWithTool(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer, tool catalogsigning.Tool) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	case "publish":
		return runPublish(ctx, arguments[1:], stdin, stdout, stderr, tool)
	default:
		fmt.Fprintf(stderr, "temper-catalog: unknown verb %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
}

func readRegularFile(path string, limit int64, label string) ([]byte, fs.FileInfo, error) {
	if path == "" {
		return nil, nil, fmt.Errorf("%s path is required", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("%s must be a regular file, not a directory or symlink", label)
	}
	if info.Size() > limit {
		return nil, nil, fmt.Errorf("%s exceeds %d-byte limit", label, limit)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", label, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect opened %s: %w", label, err)
	}
	if !os.SameFile(info, openedInfo) {
		return nil, nil, fmt.Errorf("%s changed while it was being read; rerun command", label)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", label, err)
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("%s exceeds %d-byte limit", label, limit)
	}
	return data, info, nil
}

func usage(output io.Writer) {
	fmt.Fprintln(output, `usage: temper-catalog <verb> [options]

Release-only catalog publication tool.

verbs:
  publish --source FILE --root DIRECTORY [--dry-run]
           sign one platform's catalog and the next channel; seed on stdin
  availability --catalog FILE [--temper-version VERSION] [--json]
           report which presets a Temper version can use, and why not
  verify-publication --root DIRECTORY [--source CATALOG]
           verify the complete current channel-to-snapshot publication
           --source CATALOG also requires exact authored catalog bytes`)
}
