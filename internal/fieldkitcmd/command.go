// Package fieldkitcmd exposes Temper's read-only material binding to Field Kit.
// It reads only explicitly named Temper state and never selects an experiment,
// consults a catalog, or mutates a root.
package fieldkitcmd

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

	"github.com/temper-sh/temper/internal/fieldkitbinding"
	"github.com/temper-sh/temper/internal/machine"
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receiptstore"
)

type FactsDetector func(context.Context) (machine.Facts, error)
type BinaryReader func() ([]byte, error)

type Command struct {
	detectFacts FactsDetector
	readBinary  BinaryReader
}

func New(detectFacts FactsDetector, readBinary BinaryReader) (Command, error) {
	if detectFacts == nil || readBinary == nil {
		return Command{}, errors.New("field-kit command requires facts and binary readers")
	}
	return Command{detectFacts: detectFacts, readBinary: readBinary}, nil
}

func (c Command) Run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h" {
		usage(stdout)
		return 0
	}
	if arguments[0] != "bind" {
		fmt.Fprintf(stderr, "temper field-kit: unknown command %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
	return c.runBind(ctx, arguments[1:], stdout, stderr)
}

func (c Command) runBind(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper field-kit bind", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "explicit Temper data root")
	manifestLock := flags.String("manifest-lock", "", "canonical Temper manifest lock")
	generation := flags.String("generation", "", "exact rendered generation SHA-256")
	var installations installationFlags
	flags.Var(&installations, "installation", "ordered installation-id=software-lock-path; repeatable")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" || *manifestLock == "" || *generation == "" || len(installations) == 0 {
		fmt.Fprintln(stderr, "temper field-kit bind: --root, --manifest-lock, --generation, and at least one --installation are required")
		usage(stderr)
		return 2
	}
	binary, err := c.readBinary()
	if err != nil {
		fmt.Fprintf(stderr, "temper field-kit bind: read executing Temper binary: %v\n", err)
		return 1
	}
	facts, err := c.detectFacts(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "temper field-kit bind: detect machine facts: %v\n", err)
		return 1
	}
	manifestData, err := readRegular(*manifestLock)
	if err != nil {
		fmt.Fprintf(stderr, "temper field-kit bind: read manifest lock: %v\n", err)
		return 1
	}
	inputs := fieldkitbinding.Inputs{
		TemperBinary: binary, Machine: facts, ManifestLock: manifestData,
		RenderedGeneration: *generation,
		Installations:      make([]fieldkitbinding.InstallationInput, 0, len(installations)),
	}
	for _, declared := range installations {
		id, path, _ := strings.Cut(declared, "=")
		lockData, err := readRegular(path)
		if err != nil {
			fmt.Fprintf(stderr, "temper field-kit bind: read installation %q software lock: %v\n", id, err)
			return 1
		}
		locked, err := softwarelock.Parse(lockData)
		if err != nil {
			fmt.Fprintf(stderr, "temper field-kit bind: parse installation %q software lock: %v\n", id, err)
			return 1
		}
		receipted, err := receiptstore.Read(*root, id)
		if err != nil {
			fmt.Fprintf(stderr, "temper field-kit bind: read installation %q receipt: %v\n", id, err)
			return 1
		}
		if !receipted.Exists() {
			fmt.Fprintf(stderr, "temper field-kit bind: installation %q has no receipt below the explicit root\n", id)
			return 1
		}
		inputs.Installations = append(inputs.Installations, fieldkitbinding.InstallationInput{
			Lock: locked, ReceiptData: receipted.Data,
		})
	}
	document, err := fieldkitbinding.Build(inputs)
	if err != nil {
		fmt.Fprintf(stderr, "temper field-kit bind: %v\n", err)
		return 1
	}
	data, err := fieldkitbinding.Marshal(document)
	if err != nil {
		fmt.Fprintf(stderr, "temper field-kit bind: encode binding: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(data); err != nil {
		fmt.Fprintf(stderr, "temper field-kit bind: write output: %v\n", err)
		return 1
	}
	return 0
}

type installationFlags []string

func (f *installationFlags) String() string { return strings.Join(*f, ",") }
func (f *installationFlags) Set(value string) error {
	id, path, found := strings.Cut(value, "=")
	if !found || id == "" || path == "" || filepath.Base(path) == "." {
		return errors.New("installation must be installation-id=software-lock-path")
	}
	for _, existing := range *f {
		existingID, _, _ := strings.Cut(existing, "=")
		if existingID == id {
			return fmt.Errorf("duplicate installation %q", id)
		}
	}
	*f = append(*f, value)
	return nil
}

func ReadExecutingBinary() ([]byte, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return readRegular(path)
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return nil, errors.New("expected a regular file without symlink indirection")
	}
	return os.ReadFile(path)
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: temper field-kit bind --root PATH --manifest-lock PATH --generation SHA256 --installation ID=LOCK [--installation ID=LOCK ...]")
}
