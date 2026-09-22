// Package catalogcmd owns the additive local catalog and execution-lock CLI.
// Runtime composes the existing effect primitives from a direct execution lock;
// export remains a compatibility surface for issued clients.
package catalogcmd

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
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/catalog/distribution"
	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/adapter/upstreamrelease"
	"github.com/temper-sh/temper/internal/software/catalogsource"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		usage(stderr)
		return 2
	}
	switch args[0] + " " + args[1] {
	case "catalog update", "catalog inspect", "catalog select", "catalog rollback":
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
		return compile(ctx, args[2:], stdout, stderr, reader)
	case "execution export":
		return export(ctx, args[2:], stdout, stderr)
	default:
		usage(stderr)
		return 2
	}
}

func compile(ctx context.Context, args []string, stdout, stderr io.Writer, reader upstreamrelease.ArtifactReader) int {
	f := flag.NewFlagSet("temper catalog compile", flag.ContinueOnError)
	f.SetOutput(stderr)
	catalogPath := f.String("catalog", "", "explicit local catalog snapshot")
	root := f.String("root", "", "Temper root containing a verified active catalog")
	selectionPath := f.String("selection", "", "user-owned selection")
	target := f.String("target", "", "portable target, currently darwin/arm64")
	out := f.String("out", "", "new execution lock path")
	dry := f.Bool("dry-run", false, "validate and report without writes")
	jsonOutput := f.Bool("json", false, "print the execution-input contract as JSON")
	softwareChoice := f.String("software", "recorded", "software version choice: recorded, latest or tested")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 || (*catalogPath == "") == (*root == "") || *selectionPath == "" || *out == "" || *target != "darwin/arm64" {
		usage(stderr)
		return 2
	}
	d, publishedDigest, err := readCompileCatalog(*catalogPath, *root)
	if err != nil {
		return failed(stderr, err)
	}
	raw, err := os.ReadFile(*selectionPath)
	if err != nil {
		return failed(stderr, err)
	}
	s, err := catalog.ParseSelection(raw)
	if err != nil {
		return failed(stderr, err)
	}
	d, err = catalog.ResolveSoftware(ctx, d, s, *softwareChoice, reader)
	if err != nil {
		return failed(stderr, err)
	}
	l, err := catalog.Compile(d, s, software.Target{OS: "darwin", Arch: "arm64"})
	if err != nil {
		return failed(stderr, err)
	}
	if publishedDigest != "" {
		// Preserve the authenticated source identity even when latest/tested
		// resolution changes the software material inside the new lock.
		l.SourceSnapshotSHA256 = publishedDigest
	}
	raw, err = catalog.MarshalLock(l)
	if err != nil {
		return failed(stderr, err)
	}
	changed, err := publishFile(ctx, *out, raw, *dry)
	if err != nil {
		return failed(stderr, err)
	}
	if *jsonOutput {
		return encode(stdout, stderr, map[string]any{"schema": "temper-catalog-compilation/v1", "profile": s.Profile, "execution_digest": l.Digests.Profile, "path": *out, "changed": changed, "dry_run": *dry})
	}
	fmt.Fprintf(stdout, "RESULT catalog-compile %s profile=%s execution_digest=%s path=%q\n", status(changed, *dry), s.Profile, l.Digests.Profile, *out)
	return 0
}

func readCompileCatalog(path, root string) (catalog.Document, string, error) {
	if root != "" {
		trust, err := catalogtrust.Production()
		if err != nil {
			return catalog.Document{}, "", err
		}
		snapshot, err := distribution.Read(root, trust)
		if err != nil {
			return catalog.Document{}, "", err
		}
		return snapshot.Document, snapshot.SHA256, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return catalog.Document{}, "", err
	}
	d, err := catalog.Parse(raw)
	return d, "", err
}

func export(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("temper execution export", flag.ContinueOnError)
	f.SetOutput(stderr)
	lockPath := f.String("lock", "", "self-contained execution lock")
	out := f.String("out", "", "directory for derived primitive inputs")
	dry := f.Bool("dry-run", false, "validate and report without writes")
	jsonOutput := f.Bool("json", false, "print the execution-input contract as JSON")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 || *lockPath == "" || *out == "" {
		usage(stderr)
		return 2
	}
	raw, err := os.ReadFile(*lockPath)
	if err != nil {
		return failed(stderr, err)
	}
	l, err := catalog.ParseLock(raw)
	if err != nil {
		return failed(stderr, err)
	}
	p, err := l.Projections()
	if err != nil {
		return failed(stderr, err)
	}
	files, err := p.Files()
	if err != nil {
		return failed(stderr, err)
	}
	changed, err := publishDirectory(ctx, *out, files, *dry)
	if err != nil {
		return failed(stderr, err)
	}
	if *jsonOutput {
		inputs := map[string]map[string]string{}
		for name, data := range files {
			sum := sha256.Sum256(data)
			inputs[name] = map[string]string{"path": filepath.Join(*out, name), "sha256": hex.EncodeToString(sum[:])}
		}
		lockSum := sha256.Sum256(raw)
		return encode(stdout, stderr, map[string]any{"schema": "temper-execution-inputs/v1", "profile": l.Selection.Profile, "execution_digest": l.Digests.Profile, "lock_sha256": hex.EncodeToString(lockSum[:]), "layouts": sortedKeys(l.Records.Layouts), "inputs": inputs, "changed": changed, "dry_run": *dry})
	}
	fmt.Fprintf(stdout, "RESULT execution-export %s profile=%s execution_digest=%s path=%q\n", status(changed, *dry), l.Selection.Profile, l.Digests.Profile, *out)
	for _, name := range sortedKeys(files) {
		fmt.Fprintf(stdout, "INPUT %s\n", filepath.Join(*out, name))
	}
	return 0
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

// Each file publishes atomically without replacement. A crash between exported
// files leaves exact reusable inputs; a later export verifies them and fills only
// absent files. Callers may consume the set only after a successful command.
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

func publishDirectory(ctx context.Context, path string, files map[string][]byte, dry bool) (bool, error) {
	info, err := os.Lstat(path)
	absent := errors.Is(err, fs.ErrNotExist)
	if err != nil && !absent {
		return false, err
	}
	if !absent {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("export destination must be a real directory")
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			data, ok := files[entry.Name()]
			if !ok {
				return false, fmt.Errorf("export directory contains unrelated path %q", entry.Name())
			}
			if _, err := inspectFile(filepath.Join(path, entry.Name()), data); err != nil {
				return false, err
			}
		}
	}
	changed := absent
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if absent {
		parent := filepath.Dir(path)
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("output parent %q must already be a real directory", parent)
		}
		if dry {
			return true, nil
		}
	}
	if absent && !dry {
		if err := os.Mkdir(path, 0o755); err != nil {
			return false, err
		}
		if err := syncDirectory(filepath.Dir(path)); err != nil {
			return true, err
		}
	}
	for _, name := range sortedKeys(files) {
		wrote, err := publishFile(ctx, filepath.Join(path, name), files[name], dry)
		changed = changed || wrote
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
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
func failed(w io.Writer, err error) int {
	fmt.Fprintf(w, "temper catalog/execution: %v\n", err)
	return 1
}
func usage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`Usage:
  temper catalog update --root ROOT [--dry-run] [--json]
  temper catalog inspect --root ROOT [--profile ID] [--json]
  temper catalog select --root ROOT --profile ID --out SELECTION [--dry-run] [--json]
  temper catalog rollback --root ROOT --snapshot SHA256 [--dry-run] [--json]
  temper catalog compile (--catalog FILE | --root ROOT) --selection FILE --target darwin/arm64 --out FILE [--software recorded|latest|tested] [--dry-run] [--json]
  temper execution export --lock FILE --out DIRECTORY [--dry-run] [--json]
Only catalog update retrieves a publication. Compilation with latest/tested
software resolves upstream releases explicitly. These commands do not install
or start anything.`))
}
