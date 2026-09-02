// Package probecmd launches one receipt-bound llama-swap process in the
// foreground for an explicitly isolated probe. It does not own production
// service state, launchd, ports, or recovery policy.
package probecmd

import (
	"context"
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

	"github.com/temper-sh/temper/internal/datadir"
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
}

// Runner is deliberately narrower than os/exec so command tests can prove
// that validation happens before any process effect.
type Runner interface {
	Run(context.Context, Invocation, io.Writer, io.Writer) error
}

type Command struct {
	runner Runner
}

func New(runner Runner) (Command, error) {
	if runner == nil {
		return Command{}, errors.New("probe process runner is required")
	}
	return Command{runner: runner}, nil
}

func (c Command) Run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	switch arguments[0] {
	case "serve":
		return c.runServe(ctx, arguments[1:], stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "temper probe: unknown command %q\n\n", arguments[0])
		usage(stderr)
		return 2
	}
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
}
