// Package catalogcmd owns the catalog and direct execution-lock CLI.
package catalogcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/buildinfo"
	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 2 && isHelp(args[1]) {
		usage(stdout)
		return 0
	}
	if len(args) < 2 {
		usage(stderr)
		return 2
	}
	switch args[0] + " " + args[1] {
	case "catalog describe":
		return describe(ctx, args[2:], stdout, stderr)
	case "catalog update", "catalog inspect", "catalog rollback":
		trust, err := catalogtrust.Production()
		if err != nil {
			return failed(stderr, err)
		}
		source, err := catalogsource.NewProductionHTTPS(&http.Client{Timeout: 30 * time.Second})
		if err != nil {
			return failed(stderr, err)
		}
		return runDistribution(ctx, args[1:], stdout, stderr, trust, source)
	case "catalog compile":
		reader, err := upstreamrelease.NewHTTPReader(&http.Client{Timeout: 5 * time.Minute})
		if err != nil {
			return failed(stderr, err)
		}
		trust, err := catalogtrust.Production()
		if err != nil {
			return failed(stderr, err)
		}
		return compile(ctx, args[2:], stdout, stderr, reader, trust)
	default:
		usage(stderr)
		return 2
	}
}

func compile(ctx context.Context, args []string, stdout, stderr io.Writer, reader upstreamrelease.ArtifactReader, trust publication.TrustRoot) int {
	f := flag.NewFlagSet("temper catalog compile", flag.ContinueOnError)
	f.SetOutput(stderr)
	catalogPath := f.String("catalog", "", "local catalog file, used as is, no signature check")
	root := f.String("root", "", "Temper root containing a verified active catalog")
	preset := f.String("preset", "", "explicit catalog preset")
	target := f.String("target", "", "portable target, currently darwin/arm64")
	out := f.String("out", "", "new execution lock path")
	dry := f.Bool("dry-run", false, "validate and report without writes")
	jsonOutput := f.Bool("json", false, "print the execution-input contract as JSON")
	softwareChoice := f.String("software", catalog.SoftwareStable, "software choice: stable (the catalog's tested releases) or latest")
	memoryGiB := f.Int("memory-gib", 0, "select the profile a Mac with this much memory (GiB) uses; default: the smallest")
	if err := f.Parse(args); err != nil {
		return parseFailure(err)
	}
	if f.NArg() != 0 || (*catalogPath == "") == (*root == "") || *preset == "" || *out == "" || *target != "darwin/arm64" || *memoryGiB < 0 {
		usage(stderr)
		return 2
	}
	d, err := readCompileCatalog(*catalogPath, *root, trust)
	if err != nil {
		return failed(stderr, err)
	}
	d, _, err = catalog.ResolveSoftware(ctx, d, *preset, *softwareChoice, &catalog.LatestReleases{Reader: reader}, false)
	if err != nil {
		return failed(stderr, err)
	}
	l, err := catalog.CompilePreset(d, *preset, *memoryGiB, 0, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		return failed(stderr, err)
	}
	raw, err := catalog.MarshalLock(l)
	if err != nil {
		return failed(stderr, err)
	}
	changed, err := publishFile(ctx, *out, raw, *dry)
	if err != nil {
		return failed(stderr, err)
	}
	if *jsonOutput {
		return encode(stdout, stderr, map[string]any{"schema": "temper-catalog-compilation/v1", "preset": *preset, "profile_min_memory_gib": l.Records.Presets[*preset].Profiles[0].MinMemoryGiB, "path": *out, "changed": changed, "dry_run": *dry})
	}
	fmt.Fprintf(stdout, "RESULT catalog-compile %s preset=%s profile=%dgib path=%q\n", status(changed, *dry), *preset, l.Records.Presets[*preset].Profiles[0].MinMemoryGiB, *out)
	return 0
}

func readCompileCatalog(path, root string, trust publication.TrustRoot) (catalog.Document, error) {
	if root != "" {
		snapshot, err := distribution.Read(root, trust)
		if err != nil {
			return catalog.Document{}, err
		}
		return snapshot.Document, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return catalog.Document{}, err
	}
	view, err := catalog.Read(raw, buildinfo.Version)
	return view.Document, err
}

func inspectFile(path string, data []byte) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("destination %q must be a regular file", path)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(current, data) {
		return false, fmt.Errorf("destination %q differs; choose a new path for the new identity", path)
	}
	return true, nil
}

// Publish one immutable output atomically. Replays verify existing bytes;
// different content and nonregular destinations are refused.
// A successful return means the output is ready to consume.
func publishFile(ctx context.Context, path string, data []byte, dry bool) (bool, error) {
	exists, err := inspectFile(path, data)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("output parent %q must already be a real directory", parent)
	}
	if dry {
		return true, nil
	}
	f, err := os.CreateTemp(parent, ".temper-export-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o644); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if err = os.Link(f.Name(), path); err != nil {
		if same, readErr := inspectFile(path, data); readErr == nil && same {
			return false, nil
		}
		return false, fmt.Errorf("publish output without replacement: %w", err)
	}
	if err = syncDirectory(parent); err != nil {
		return true, fmt.Errorf("output published but directory sync failed: %w", err)
	}
	return true, nil
}

func syncDirectory(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func encode(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return failed(stderr, err)
	}
	return 0
}
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func status(changed, dry bool) string {
	if !changed {
		return "unchanged"
	}
	if dry {
		return "would-write"
	}
	return "written"
}

// parseFailure is the exit code for a flag error; asking for help succeeds.
func parseFailure(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	return 2
}

func isHelp(arg string) bool { return arg == "help" || arg == "--help" || arg == "-h" }

func failed(w io.Writer, err error) int {
	fmt.Fprintf(w, "temper catalog/execution: %v\n", err)
	return 1
}
func usage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`Usage:
  temper catalog update --root ROOT [--dry-run] [--json]
  temper catalog inspect --root ROOT [--preset ID] [--json]
  temper catalog rollback --root ROOT --snapshot SHA256 [--dry-run] [--json]
  temper catalog compile (--catalog FILE | --root ROOT) --preset ID --target darwin/arm64 --out FILE [--software stable|latest] [--memory-gib N] [--dry-run] [--json]
  temper catalog describe --catalog FILE --preset ID (--description TEXT | --description-file FILE) [--assessment-url URL] [--if-empty] [--dry-run]
Only catalog update retrieves a publication. Compilation with latest
software resolves upstream releases explicitly. These commands do not install
or start anything.`))
}

func runtimeUsage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`Usage:
  temper execution configure --lock FILE --preset ID --context N --max-output N [--compute cpu|gpu] [--max-memory BYTES] --out FILE [--dry-run]
  temper execution inspect --lock FILE
  temper execution prepare|render|paths|remove --lock FILE --root PATH --installation ID [--dry-run]
  temper execution serve --lock FILE --root PATH --installation ID --generation SHA256 --status-file FILE [--listen 127.0.0.1:PORT] [--dry-run]
These commands act on one exact execution lock.`))
}
