// Command temper-catalog is the retained release-side signer and verifier for
// Temper's software catalog publications. It is deliberately separate from
// the end-user temper binary and never discovers private key material.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/temper-sh/temper/internal/software/adapter"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	tool, err := newProductionTool()
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog: construct release tool: %v\n", err)
		return 1
	}
	return runWithTool(ctx, arguments, stdin, stdout, stderr, tool)
}

func newProductionTool() (catalogsigning.Tool, error) {
	trust, err := catalogtrust.Production()
	if err != nil {
		return catalogsigning.Tool{}, err
	}
	capabilities, err := adapter.NewRegistry(upstreamrelease.Descriptor())
	if err != nil {
		return catalogsigning.Tool{}, err
	}
	return catalogsigning.New(catalogtrust.ProductionKeyID, trust, capabilities)
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
	case "sign":
		return runSign(ctx, arguments[1:], stdin, stdout, stderr, tool)
	case "verify":
		return runVerify(arguments[1:], stdout, stderr, tool)
	default:
		fmt.Fprintf(stderr, "temper-catalog: unknown verb %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
}

func runSign(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer, tool catalogsigning.Tool) int {
	flags := flag.NewFlagSet("temper-catalog sign", flag.ContinueOnError)
	flags.SetOutput(stderr)
	kindValue := flags.String("kind", "", "publication kind: catalog or channel")
	channel := flags.String("channel", "", "expected channel name for a channel publication")
	artifactPath := flags.String("artifact", "", "exact publication artifact to sign")
	outputPath := flags.String("output", "", "detached signature output path")
	replace := flags.Bool("replace", false, "atomically replace an existing different signature")
	dryRun := flags.Bool("dry-run", false, "validate and report without writing the signature")
	flags.Usage = func() { signUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *kindValue == "" || *artifactPath == "" || *outputPath == "" {
		if flags.NArg() != 0 {
			fmt.Fprintf(stderr, "temper-catalog sign: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		} else {
			fmt.Fprintln(stderr, "temper-catalog sign: --kind, --artifact, and --output are required")
		}
		signUsage(stderr)
		return 2
	}
	kind, err := catalogsigning.ParseKind(*kindValue)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 2
	}
	artifact, artifactInfo, err := readRegularFile(*artifactPath, artifactLimit(kind), "catalog publication artifact")
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 1
	}
	if err := refuseSameFile(*artifactPath, artifactInfo, *outputPath); err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 1
	}
	seedInput, err := io.ReadAll(io.LimitReader(stdin, catalogsigning.MaxSeedInputBytes+1))
	if err != nil {
		fmt.Fprintln(stderr, "temper-catalog sign: read catalog signing seed from stdin")
		return 1
	}
	defer clear(seedInput)
	seed, err := catalogsigning.ParseSeed(seedInput)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 1
	}
	defer clear(seed)
	envelope, err := tool.Sign(kind, *channel, artifact, seed)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 1
	}
	keyID, err := tool.Verify(kind, *channel, artifact, envelope)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: verify candidate signature: %v\n", err)
		return 1
	}
	output, err := catalogsigning.ReadOutput(*outputPath)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 1
	}
	change, err := output.Plan(envelope, *replace)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
		return 1
	}
	status := string(change)
	if *dryRun && change != catalogsigning.ChangeUnchanged {
		switch change {
		case catalogsigning.ChangeCreated:
			status = "would-create"
		case catalogsigning.ChangeReplaced:
			status = "would-replace"
		}
	} else if !*dryRun {
		change, err = output.Commit(ctx, envelope, *replace)
		if err != nil {
			fmt.Fprintf(stderr, "temper-catalog sign: %v\n", err)
			return 1
		}
		status = string(change)
	}
	fmt.Fprintf(stdout, "RESULT catalog-sign %s kind=%s key=%s\n", status, kind, keyID)
	return 0
}

func runVerify(arguments []string, stdout, stderr io.Writer, tool catalogsigning.Tool) int {
	flags := flag.NewFlagSet("temper-catalog verify", flag.ContinueOnError)
	flags.SetOutput(stderr)
	kindValue := flags.String("kind", "", "publication kind: catalog or channel")
	channel := flags.String("channel", "", "expected channel name for a channel publication")
	artifactPath := flags.String("artifact", "", "exact publication artifact to verify")
	signaturePath := flags.String("signature", "", "detached signature path")
	flags.Usage = func() { verifyUsage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || *kindValue == "" || *artifactPath == "" || *signaturePath == "" {
		if flags.NArg() != 0 {
			fmt.Fprintf(stderr, "temper-catalog verify: unexpected arguments: %s\n\n", strings.Join(flags.Args(), " "))
		} else {
			fmt.Fprintln(stderr, "temper-catalog verify: --kind, --artifact, and --signature are required")
		}
		verifyUsage(stderr)
		return 2
	}
	kind, err := catalogsigning.ParseKind(*kindValue)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog verify: %v\n", err)
		return 2
	}
	artifact, _, err := readRegularFile(*artifactPath, artifactLimit(kind), "catalog publication artifact")
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog verify: %v\n", err)
		return 1
	}
	envelope, _, err := readRegularFile(*signaturePath, catalogsource.MaxSignatureBytes, "catalog publication signature")
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog verify: %v\n", err)
		return 1
	}
	keyID, err := tool.Verify(kind, *channel, artifact, envelope)
	if err != nil {
		fmt.Fprintf(stderr, "temper-catalog verify: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "RESULT catalog-verify valid kind=%s key=%s\n", kind, keyID)
	return 0
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

func refuseSameFile(artifactPath string, artifactInfo fs.FileInfo, outputPath string) error {
	outputInfo, err := os.Lstat(outputPath)
	if err == nil {
		if os.SameFile(artifactInfo, outputInfo) {
			return errors.New("catalog signature output must not be the artifact being signed")
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect catalog signature output: %w", err)
	}
	artifactAbsolute, err := filepath.Abs(artifactPath)
	if err != nil {
		return fmt.Errorf("resolve catalog artifact path: %w", err)
	}
	outputAbsolute, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve catalog signature output path: %w", err)
	}
	if filepath.Clean(artifactAbsolute) == filepath.Clean(outputAbsolute) {
		return errors.New("catalog signature output must not be the artifact being signed")
	}
	return nil
}

func artifactLimit(kind catalogsigning.Kind) int64 {
	if kind == catalogsigning.KindChannel {
		return catalogsource.MaxChannelBytes
	}
	return catalogsource.MaxCatalogBytes
}

func usage(output io.Writer) {
	fmt.Fprintln(output, `usage: temper-catalog <verb> [options]

Release-only software catalog publication tool.

verbs:
  sign     validate and sign exact artifact bytes with a base64 seed from stdin
  verify   validate an artifact and its detached production signature`)
}

func signUsage(output io.Writer) {
	fmt.Fprintln(output, `usage: temper-catalog sign --kind catalog|channel --artifact PATH --output PATH [options]

options:
  --channel ID   required for kind=channel; invalid for kind=catalog
  --replace      replace an existing different detached signature
  --dry-run      validate and report without writing

The canonical base64 Ed25519 seed is read only from stdin.`)
}

func verifyUsage(output io.Writer) {
	fmt.Fprintln(output, `usage: temper-catalog verify --kind catalog|channel --artifact PATH --signature PATH [options]

options:
  --channel ID   required for kind=channel; invalid for kind=catalog`)
}
