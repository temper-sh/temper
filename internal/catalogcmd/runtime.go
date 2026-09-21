package catalogcmd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/probecmd"
	"github.com/temper-sh/temper/internal/render"
)

// Dispatch invokes the existing CLI primitives in-process. Temporary legacy
// projections remain an implementation detail; the caller supplies one lock.
type Dispatch func(context.Context, []string, io.Writer, io.Writer) int

func Runtime(ctx context.Context, args []string, stdout, stderr io.Writer, dispatch Dispatch) int {
	if len(args) == 0 {
		return failed(stderr, errors.New("execution operation required"))
	}
	operation := args[0]
	switch operation {
	case "inspect", "prepare", "render", "serve", "remove":
	default:
		return failed(stderr, fmt.Errorf("unknown execution operation %q", operation))
	}
	f := flag.NewFlagSet("temper execution "+operation, flag.ContinueOnError)
	f.SetOutput(stderr)
	lockPath := f.String("lock", "", "exact execution lock")
	root := f.String("root", "", "explicit Temper root")
	installation := f.String("installation", "", "exact installation ID")
	generation := f.String("generation", "", "rendered generation for serving")
	listen := f.String("listen", "127.0.0.1:8080", "loopback listener")
	statusFile := f.String("status-file", "", "new supervision status path")
	dry := f.Bool("dry-run", false, "validate without effects")
	if err := f.Parse(args[1:]); err != nil {
		return 2
	}
	if f.NArg() != 0 || *lockPath == "" {
		return failed(stderr, errors.New("--lock is required"))
	}
	info, err := os.Lstat(*lockPath)
	if err != nil {
		return failed(stderr, err)
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return failed(stderr, errors.New("execution lock must be a regular file no larger than 4 MiB"))
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
	sum := sha256.Sum256(raw)
	identity := map[string]any{"schema": "temper-execution/v1", "lock_sha256": hex.EncodeToString(sum[:]), "execution_digest": l.Digests.Profile,
		"profile": l.Selection.Profile, "layouts": sortedKeys(l.Records.Layouts), "request_defaults": p.RequestDefaults}
	if operation == "inspect" {
		return encode(stdout, stderr, identity)
	}
	if *root == "" || *installation == "" {
		return failed(stderr, errors.New("--root and --installation are required"))
	}
	resolved, err := datadir.Resolve(*root)
	if err != nil {
		return failed(stderr, err)
	}
	if !regexp.MustCompile(`^[a-z0-9]+(?:[.-][a-z0-9]+)*$`).MatchString(*installation) {
		return failed(stderr, errors.New("invalid installation ID"))
	}
	if operation == "serve" && (!regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(*generation) || *statusFile == "") {
		return failed(stderr, errors.New("serve requires --generation and a new --status-file"))
	}
	if operation == "serve" {
		if len(l.Records.Layouts) != 1 {
			return failed(stderr, errors.New("supervised serving currently requires exactly one layout"))
		}
		supervision := probecmd.Supervision{StatusPath: *statusFile, Root: resolved, Installation: *installation, Generation: *generation, Listen: *listen}
		if err := supervision.Validate(); err != nil {
			return failed(stderr, err)
		}
		bundle, err := render.Build(render.Inputs{Manifest: p.Manifest, Lock: p.Artifacts, Mode: l.Selection.Profile, Root: resolved})
		if err != nil {
			return failed(stderr, err)
		}
		if bundle.Digest() != *generation {
			return failed(stderr, errors.New("generation differs from the supplied execution lock and root"))
		}
		if !*dry {
			for _, artifact := range bundle.Artifacts {
				path := filepath.Join(resolved, "rendered", "generations", *generation, artifact.Path)
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() {
					return failed(stderr, fmt.Errorf("rendered artifact is absent or unsafe: %s", artifact.Path))
				}
				actual, err := os.ReadFile(path)
				if err != nil {
					return failed(stderr, err)
				}
				if !bytes.Equal(actual, artifact.Data) {
					return failed(stderr, fmt.Errorf("rendered artifact differs from execution lock: %s", artifact.Path))
				}
			}
		}
	}
	if *dry {
		return encode(stdout, stderr, map[string]any{"schema": "temper-execution-plan/v1", "operation": operation, "execution": identity, "root": resolved, "installation": *installation, "dry_run": true})
	}
	if err := ctx.Err(); err != nil {
		return failed(stderr, err)
	}
	files, err := p.Files()
	if err != nil {
		return failed(stderr, err)
	}
	temporary, err := os.MkdirTemp("", "temper-execution-*")
	if err != nil {
		return failed(stderr, err)
	}
	defer os.RemoveAll(temporary)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(temporary, name), data, 0o600); err != nil {
			return failed(stderr, err)
		}
	}
	softwareLock := filepath.Join(temporary, "software.lock.yaml")
	manifestLock := filepath.Join(temporary, "manifest.lock.yaml")
	manifest := filepath.Join(temporary, "manifest.yaml")
	softwareArgs := []string{"--root", resolved, "--installation", *installation, "--lock", softwareLock}
	modelArgs := []string{"--root", resolved, "--manifest", manifest, "--lock", manifestLock}
	if operation == "remove" {
		return dispatch(ctx, append([]string{"software", "remove"}, softwareArgs...), stdout, stderr)
	}
	if operation == "serve" {
		return dispatch(ctx, []string{"probe", "serve", "--root", resolved, "--installation", *installation,
			"--software-lock", softwareLock, "--generation", *generation, "--listen", *listen, "--status-file", *statusFile}, stdout, stderr)
	}
	run := func(arguments []string) ([]byte, error) {
		var output bytes.Buffer
		if code := dispatch(ctx, arguments, &output, stderr); code != 0 {
			_, _ = stderr.Write(output.Bytes())
			return nil, fmt.Errorf("execution %s failed at %s (exit %d)", operation, arguments[0], code)
		}
		return output.Bytes(), nil
	}
	if operation == "prepare" {
		if _, err := run(append([]string{"software", "install"}, softwareArgs...)); err != nil {
			return failed(stderr, err)
		}
		for _, layout := range sortedKeys(l.Records.Layouts) {
			if _, err := run(append([]string{"fetch", layout}, modelArgs...)); err != nil {
				return failed(stderr, err)
			}
		}
	}
	if _, err := run(append([]string{"software", "check"}, softwareArgs...)); err != nil {
		return failed(stderr, err)
	}
	output, err := run(append(append([]string{"apply"}, modelArgs...), "--mode", l.Selection.Profile))
	if err != nil {
		return failed(stderr, err)
	}
	matched := regexp.MustCompile(`(?m)^RESULT apply [^\n]*generation=([0-9a-f]{64})$`).FindSubmatch(output)
	if len(matched) != 2 {
		return failed(stderr, errors.New("apply returned no generation"))
	}
	if _, err := run(append(append([]string{"check"}, modelArgs...), "--mode", l.Selection.Profile, "--verify")); err != nil {
		return failed(stderr, err)
	}
	binding, err := run([]string{"field-kit", "bind", "--root", resolved, "--manifest-lock", manifestLock,
		"--generation", string(matched[1]), "--installation", *installation + "=" + softwareLock})
	if err != nil {
		return failed(stderr, err)
	}
	return encode(stdout, stderr, map[string]any{"schema": "temper-execution-material/v1", "execution": identity, "generation": string(matched[1]), "binding": string(binding)})
}
