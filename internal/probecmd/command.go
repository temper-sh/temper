// Package probecmd plans and supervises receipt-bound engine processes: one
// foreground llama-swap process for execution serving, and the process
// identities managed layouts observe. It does not own production service
// state, launchd, ports, or protocol policy.
package probecmd

import (
	"context"
	"errors"
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
	softwarelock "github.com/temper-sh/temper/internal/software/lockfile"
	"github.com/temper-sh/temper/internal/software/receipt"
	"github.com/temper-sh/temper/internal/software/receiptstore"
)

var generationPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Invocation is the complete, validated foreground process boundary.
type Invocation struct {
	// EnginePort is the port the router gives the served preset's engine.
	EnginePort            int
	PythonMultiprocessing bool
	PythonCrashLogSHA256  string
	Path                  string
	Arguments             []string
	Environment           []string
	EnginePath            string
	EngineArguments       []string
	FrontendPath          string
	FrontendArguments     []string
	Supervision           *Supervision
}

// Runner is deliberately narrower than os/exec so command tests can prove
// that validation happens before any process effect.
type Runner interface {
	Run(context.Context, Invocation, io.Writer, io.Writer) error
}

type Options struct {
	Root         string
	Installation string
	SoftwareLock softwarelock.Document
	Generation   string
	Listen       string
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

	if err := options.SoftwareLock.Validate(); err != nil {
		return Invocation{}, fmt.Errorf("software lock: %w", err)
	}
	installed, err := receiptstore.Read(root, options.Installation)
	if err != nil {
		return Invocation{}, fmt.Errorf("read software receipt: %w", err)
	}
	if !installed.Exists() {
		return Invocation{}, fmt.Errorf("software installation %q has no receipt", options.Installation)
	}
	if err := installed.Document.ValidateAgainst(options.SoftwareLock, installplan.Installation{ID: options.Installation, Root: root}); err != nil {
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

	// Serving runs one preset, so its engine has the first port of the root's block.
	enginePort := datadir.StartPort(root)
	router := ""
	enginePath := ""
	frontendPath := ""
	var engineArguments, frontendArguments []string
	pythonMultiprocessing := false
	pythonCrashLogSHA256 := ""
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
		if requirement.Package == "llama-cpp" || requirement.Package == "crispembed" {
			enginePath = executable
		}
		if (requirement.Package == "rapid-mlx" || requirement.Package == "vllm-metal") && requirement.Role != "" {
			python, err := executableAt(root, options.Installation, location, "bin/python3")
			if err != nil {
				return Invocation{}, fmt.Errorf("Python engine interpreter: %w", err)
			}
			args := append([]string{executable}, requirement.Arguments...)
			for i, arg := range args {
				if arg == "${PORT}" {
					args[i] = strconv.Itoa(enginePort)
				}
			}
			enginePath = python
			if requirement.Package == "rapid-mlx" {
				engineArguments = args
				pythonCrashLogSHA256 = rapidCrashLogSHA256
			} else {
				frontendPath, frontendArguments, pythonMultiprocessing = python, args, true
			}
		}
		if requirement.Package == "splash" {
			args := append([]string(nil), requirement.Arguments...)
			for i, arg := range args {
				if arg == "${PORT}" {
					args[i] = strconv.Itoa(enginePort)
				}
			}
			if requirement.Role == "frontend" {
				frontendPath, frontendArguments = executable, args
			} else {
				enginePath, engineArguments = executable, args
			}
		}
	}

	config := filepath.Join(generationRoot, "llama-swap", "config.yaml")
	if err := regularFile(config, false); err != nil {
		return Invocation{}, fmt.Errorf("rendered config: %w", err)
	}
	return Invocation{
		EnginePort:            enginePort,
		PythonMultiprocessing: pythonMultiprocessing,
		PythonCrashLogSHA256:  pythonCrashLogSHA256,
		Path:                  router,
		EnginePath:            enginePath,
		EngineArguments:       engineArguments, FrontendPath: frontendPath, FrontendArguments: frontendArguments,
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
	if selection.Adapter != "upstream-release" && selection.Adapter != "python" {
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

// InstalledExecutable resolves an executable only through a matching immutable
// software receipt. Callers verify software content before invoking it.
func InstalledExecutable(root, installation string, locked softwarelock.Document, packageID, relative string) (string, error) {
	installed, err := receiptstore.Read(root, installation)
	if err != nil {
		return "", err
	}
	if !installed.Exists() {
		return "", errors.New("software lock and receipt are required")
	}
	if err := installed.Document.ValidateAgainst(locked, installplan.Installation{ID: installation, Root: root}); err != nil {
		return "", err
	}
	location, err := selectionLocation(installed.Document, packageID)
	if err != nil {
		return "", err
	}
	return executableAt(root, installation, location, relative)
}
