// Package probecmd exposes receipt-bound, non-policy probe primitives: one
// foreground llama-swap process and one offline GGUF tokenizer invocation.
// It does not own production service state, launchd, ports, or protocol policy.
package probecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/artifactset"
	"github.com/temper-sh/temper/internal/datadir"
	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/runtimeconfig"
	"github.com/temper-sh/temper/internal/software/installplan"
	"github.com/temper-sh/temper/internal/software/lockstore"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
)

var generationPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Invocation is the complete, validated foreground process boundary.
type Invocation struct {
	Path        string
	Arguments   []string
	Environment []string
	Input       []byte
}

// Runner is deliberately narrower than os/exec so command tests can prove
// that validation happens before any process effect.
type Runner interface {
	Run(context.Context, Invocation, io.Writer, io.Writer) error
}

type Command struct {
	runner Runner
	stdin  io.Reader
}

func New(runner Runner) (Command, error) {
	return NewWithInput(runner, strings.NewReader(""))
}

// NewWithInput constructs the probe command with an explicit input stream.
// Only the non-generating tokenizer primitive consumes it.
func NewWithInput(runner Runner, stdin io.Reader) (Command, error) {
	if runner == nil {
		return Command{}, errors.New("probe process runner is required")
	}
	if stdin == nil {
		return Command{}, errors.New("probe input stream is required")
	}
	return Command{runner: runner, stdin: stdin}, nil
}

func (c Command) Run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "serve":
		return c.runServe(ctx, arguments[1:], stdout, stderr)
	case "tokenize":
		return c.runTokenize(ctx, arguments[1:], stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "temper probe: unknown command %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
}

const maxTokenizeInputBytes = 64 << 20
const maxTokenizeOutputBytes = 64 << 20

type cappedBuffer struct {
	buffer    bytes.Buffer
	remaining int
	exceeded  bool
}

func (w *cappedBuffer) Write(value []byte) (int, error) {
	accepted := len(value)
	if accepted > w.remaining {
		accepted = w.remaining
		w.exceeded = true
	}
	if accepted > 0 {
		_, _ = w.buffer.Write(value[:accepted])
		w.remaining -= accepted
	}
	// Report the complete write so an oversized child cannot turn the bounded
	// evidence capture itself into an unbounded diagnostic stream.
	return len(value), nil
}

func (c Command) runTokenize(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper probe tokenize", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "explicit Temper data root")
	installation := flags.String("installation", "", "exact software installation id")
	softwareLock := flags.String("software-lock", "software.lock.yaml", "exact software lock path")
	manifestPath := flags.String("manifest", "manifest.yaml", "exact manifest path")
	lockPath := flags.String("lock", "manifest.lock.yaml", "exact manifest lock path")
	layout := flags.String("layout", "", "exact GGUF layout id")
	flags.Usage = func() { usage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" || *installation == "" || *layout == "" {
		fmt.Fprintln(stderr, "temper probe tokenize: --root, --installation, and --layout are required")
		return 2
	}
	input, err := io.ReadAll(io.LimitReader(c.stdin, maxTokenizeInputBytes+1))
	if err != nil {
		fmt.Fprintf(stderr, "temper probe tokenize: read input: %v\n", err)
		return 1
	}
	if len(input) > maxTokenizeInputBytes {
		fmt.Fprintf(stderr, "temper probe tokenize: input exceeds %d bytes\n", maxTokenizeInputBytes)
		return 1
	}
	invocation, err := PlanTokenize(TokenizeOptions{
		Root: *root, Installation: *installation, SoftwareLockPath: *softwareLock,
		ManifestPath: *manifestPath, LockPath: *lockPath, Layout: *layout, Input: input,
	})
	if err != nil {
		fmt.Fprintf(stderr, "temper probe tokenize: %v\n", err)
		return 1
	}
	raw := cappedBuffer{remaining: maxTokenizeOutputBytes}
	var childStderr bytes.Buffer
	if err := c.runner.Run(ctx, invocation, &raw, &childStderr); err != nil {
		if childStderr.Len() > 0 {
			_, _ = stderr.Write(childStderr.Bytes())
		}
		fmt.Fprintf(stderr, "temper probe tokenize: %v\n", err)
		return 1
	}
	if raw.exceeded {
		fmt.Fprintf(stderr, "temper probe tokenize: tokenizer output exceeds %d bytes\n", maxTokenizeOutputBytes)
		return 1
	}
	var ids []int
	decoder := json.NewDecoder(bytes.NewReader(raw.buffer.Bytes()))
	if err := decoder.Decode(&ids); err != nil {
		fmt.Fprintf(stderr, "temper probe tokenize: tokenizer returned invalid token IDs: %v\n", err)
		return 1
	}
	for _, id := range ids {
		if id < 0 {
			fmt.Fprintln(stderr, "temper probe tokenize: tokenizer returned a negative token ID")
			return 1
		}
	}
	if err := requireJSONEOF(decoder); err != nil {
		fmt.Fprintf(stderr, "temper probe tokenize: tokenizer returned invalid token IDs: %v\n", err)
		return 1
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		fmt.Fprintf(stderr, "temper probe tokenize: encode token IDs: %v\n", err)
		return 1
	}
	_, _ = stdout.Write(append(encoded, '\n'))
	return 0
}

type TokenizeOptions struct {
	Root             string
	Installation     string
	SoftwareLockPath string
	ManifestPath     string
	LockPath         string
	Layout           string
	Input            []byte
}

// PlanTokenize resolves one receipted llama-tokenize binary and the exact
// immutable GGUF selected by the supplied manifest and lock.
func PlanTokenize(options TokenizeOptions) (Invocation, error) {
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Invocation{}, err
	}
	locked, err := lockstore.Read(options.SoftwareLockPath)
	if err != nil {
		return Invocation{}, fmt.Errorf("read software lock: %w", err)
	}
	if !locked.Exists() {
		return Invocation{}, fmt.Errorf("software lock %q does not exist", options.SoftwareLockPath)
	}
	installed, err := receiptstore.Read(root, options.Installation)
	if err != nil {
		return Invocation{}, fmt.Errorf("read software receipt: %w", err)
	}
	if !installed.Exists() {
		return Invocation{}, fmt.Errorf("software installation %q has no receipt", options.Installation)
	}
	if err := installed.Document.ValidateAgainst(locked.Document, installplan.Installation{ID: options.Installation, Root: root}); err != nil {
		return Invocation{}, fmt.Errorf("validate software receipt: %w", err)
	}

	manifestData, err := os.ReadFile(options.ManifestPath)
	if err != nil {
		return Invocation{}, fmt.Errorf("read manifest: %w", err)
	}
	document, err := manifest.Parse(manifestData)
	if err != nil {
		return Invocation{}, err
	}
	lockData, err := os.ReadFile(options.LockPath)
	if err != nil {
		return Invocation{}, fmt.Errorf("read manifest lock: %w", err)
	}
	modelLock, err := lockfile.Parse(lockData)
	if err != nil {
		return Invocation{}, err
	}
	layout, ok := document.Layouts[options.Layout]
	if !ok {
		return Invocation{}, fmt.Errorf("layout %q is not declared in the manifest", options.Layout)
	}
	if layout.ModelFormat() != "gguf" || layout.Engine != "llama-server" {
		return Invocation{}, fmt.Errorf("layout %q must select a GGUF llama-server model", options.Layout)
	}
	entry, ok := modelLock.Entry(options.Layout)
	if !ok {
		return Invocation{}, fmt.Errorf("layout %q has no lock entry", options.Layout)
	}
	set, err := artifactset.New(root, options.Layout, layout, entry, document.Patches)
	if err != nil {
		return Invocation{}, err
	}
	if err := set.Verify(); err != nil {
		return Invocation{}, fmt.Errorf("verify artifact set: %w", err)
	}
	location, err := selectionLocation(installed.Document, "llama-cpp")
	if err != nil {
		return Invocation{}, err
	}
	executable, err := executableAt(root, options.Installation, location, "llama-tokenize")
	if err != nil {
		return Invocation{}, fmt.Errorf("package %q executable: %w", "llama-cpp", err)
	}
	return Invocation{
		Path: executable,
		Arguments: []string{
			"-m", set.ModelPath(), "--stdin", "--ids", "--no-bos", "--offline", "--log-disable",
		},
		Environment: []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"},
		Input:       append([]byte(nil), options.Input...),
	}, nil
}

func (c Command) runServe(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("temper probe serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "explicit Temper data root")
	installation := flags.String("installation", "", "exact software installation id")
	softwareLock := flags.String("software-lock", "software.lock.yaml", "exact software lock path")
	generation := flags.String("generation", "", "exact rendered generation digest")
	listen := flags.String("listen", "127.0.0.1:8080", "loopback listen address")
	dryRun := flags.Bool("dry-run", false, "validate the complete invocation without starting a process")
	flags.Usage = func() { usage(stderr) }
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 || *root == "" || *installation == "" || *generation == "" {
		fmt.Fprintln(stderr, "temper probe serve: --root, --installation, and --generation are required")
		return 2
	}

	invocation, err := Plan(Options{
		Root: *root, Installation: *installation, SoftwareLockPath: *softwareLock,
		Generation: *generation, Listen: *listen,
	})
	if err != nil {
		fmt.Fprintf(stderr, "temper probe serve: %v\n", err)
		return 1
	}
	if *dryRun {
		fmt.Fprintf(stdout, "RESULT probe-serve ready-to-start installation=%s generation=%s listen=%s\n", *installation, *generation, *listen)
		return 0
	}
	if err := ctx.Err(); err != nil {
		fmt.Fprintf(stderr, "temper probe serve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "RESULT probe-serve starting installation=%s generation=%s listen=%s\n", *installation, *generation, *listen)
	if err := c.runner.Run(ctx, invocation, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "temper probe serve: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "RESULT probe-serve stopped installation=%s generation=%s listen=%s\n", *installation, *generation, *listen)
	return 0
}

type Options struct {
	Root             string
	Installation     string
	SoftwareLockPath string
	Generation       string
	Listen           string
}

// Plan resolves only exact immutable identities: one canonical lock, its
// matching canonical installation receipt, and one content-addressed render.
func Plan(options Options) (Invocation, error) {
	root, err := datadir.Resolve(options.Root)
	if err != nil {
		return Invocation{}, err
	}
	if !generationPattern.MatchString(options.Generation) {
		return Invocation{}, errors.New("generation must be 64 lowercase hexadecimal characters")
	}
	if err := validateListen(options.Listen); err != nil {
		return Invocation{}, err
	}

	locked, err := lockstore.Read(options.SoftwareLockPath)
	if err != nil {
		return Invocation{}, fmt.Errorf("read software lock: %w", err)
	}
	if !locked.Exists() {
		return Invocation{}, fmt.Errorf("software lock %q does not exist", options.SoftwareLockPath)
	}
	installed, err := receiptstore.Read(root, options.Installation)
	if err != nil {
		return Invocation{}, fmt.Errorf("read software receipt: %w", err)
	}
	if !installed.Exists() {
		return Invocation{}, fmt.Errorf("software installation %q has no receipt", options.Installation)
	}
	if err := installed.Document.ValidateAgainst(locked.Document, installplan.Installation{ID: options.Installation, Root: root}); err != nil {
		return Invocation{}, fmt.Errorf("validate software receipt: %w", err)
	}

	generationRoot := filepath.Join(root, "rendered", "generations", options.Generation)
	requirementsPath := filepath.Join(generationRoot, "runtime", "requirements.json")
	if err := regularFile(requirementsPath, false); err != nil {
		return Invocation{}, fmt.Errorf("runtime requirements: %w", err)
	}
	requirementsData, err := os.ReadFile(requirementsPath)
	if err != nil {
		return Invocation{}, fmt.Errorf("read runtime requirements: %w", err)
	}
	requirements, err := runtimeconfig.Parse(requirementsData)
	if err != nil {
		return Invocation{}, err
	}

	router := ""
	var executableDirectories []string
	for _, requirement := range requirements.Requirements {
		location, err := selectionLocation(installed.Document, requirement.Package)
		if err != nil {
			return Invocation{}, err
		}
		executable, err := executableAt(root, options.Installation, location, requirement.RelativeExecutable)
		if err != nil {
			return Invocation{}, fmt.Errorf("package %q executable: %w", requirement.Package, err)
		}
		if requirement.Package == "llama-swap" {
			router = executable
		} else if !containsString(executableDirectories, filepath.Dir(executable)) {
			executableDirectories = append(executableDirectories, filepath.Dir(executable))
		}
	}

	config := filepath.Join(generationRoot, "llama-swap", "config.yaml")
	if err := regularFile(config, false); err != nil {
		return Invocation{}, fmt.Errorf("rendered config: %w", err)
	}
	return Invocation{
		Path:      router,
		Arguments: []string{"--config", config, "--listen", options.Listen},
		Environment: []string{
			"PATH=" + strings.Join(append(executableDirectories, "/usr/bin", "/bin", "/usr/sbin", "/sbin"), string(os.PathListSeparator)),
		},
	}, nil
}

func selectionLocation(document receipt.Document, packageID string) (string, error) {
	selection, ok := document.Selections[packageID]
	if !ok {
		return "", fmt.Errorf("software receipt has no %q selection", packageID)
	}
	if selection.Adapter != "upstream-release" && selection.Adapter != "uv" {
		return "", fmt.Errorf("software selection %q uses unsupported adapter %q", packageID, selection.Adapter)
	}
	unit, ok := document.Units[selection.RootUnit]
	if !ok {
		return "", fmt.Errorf("software selection %q has no receipted root unit", packageID)
	}
	return unit.Location, nil
}

func executableAt(root, installation, location, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("executable relative path is invalid")
	}
	resolvedLocation, err := filepath.EvalSymlinks(location)
	if err != nil {
		return "", fmt.Errorf("resolve installation location: %w", err)
	}
	installationRoot := filepath.Join(root, "software", "installations", installation)
	resolvedInstallationRoot, err := filepath.EvalSymlinks(installationRoot)
	if err != nil {
		return "", fmt.Errorf("resolve installation root: %w", err)
	}
	if !strictlyBelow(resolvedInstallationRoot, resolvedLocation) {
		return "", errors.New("resolved installation location escapes its named installation")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(location, relative))
	if err != nil {
		return "", err
	}
	if !strictlyBelow(resolvedLocation, path) {
		return "", errors.New("resolved executable escapes its receipted payload")
	}
	if err := regularFile(path, true); err != nil {
		return "", err
	}
	return path, nil
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func regularFile(path string, executable bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%q does not exist", path)
		}
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%q must be a regular file without symlink indirection", path)
	}
	if executable && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%q is not executable", path)
	}
	return nil
}

func validateListen(value string) error {
	host, portText, err := net.SplitHostPort(value)
	if err != nil || host != "127.0.0.1" {
		return errors.New("listen must be an IPv4 loopback address such as 127.0.0.1:8080")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1024 || port > 65535 {
		return errors.New("listen port must be between 1024 and 65535")
	}
	return nil
}

func strictlyBelow(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  temper probe serve --root PATH --installation ID --generation SHA256 [--software-lock PATH] [--listen 127.0.0.1:PORT] [--dry-run]")
	fmt.Fprintln(writer, "  temper probe tokenize --root PATH --installation ID --layout ID [--software-lock PATH] [--manifest PATH] [--lock PATH] < rendered-prompt.bin")
}
