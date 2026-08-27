// Command temper-release builds and packages a Temper release artifact. It is
// a maintainer-only tool: signing, notarization, and publication remain in the
// release workflow.
package main

import (
	"bytes"
	"debug/macho"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/temper-sh/temper/internal/releaseartifact"
)

const maxNoticeFileBytes = 8 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		usage(stderr)
		return 2
	}
	var err error
	switch arguments[0] {
	case "build":
		err = runBuild(arguments[1:], stdout, stderr)
	case "package":
		err = runPackage(arguments[1:], stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "temper-release: unknown command %q\n", arguments[0])
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "temper-release %s: %v\n", arguments[0], err)
		return 1
	}
	return 0
}

func usage(writer io.Writer) {
	fmt.Fprintln(writer, "usage:")
	fmt.Fprintln(writer, "  temper-release build --version SEMVER --output FILE [--repo DIR]")
	fmt.Fprintln(writer, "  temper-release package --version SEMVER --binary FILE --output DIR [--repo DIR]")
}

func runBuild(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "release version without leading v")
	output := flags.String("output", "", "output binary path")
	repo := flags.String("repo", ".", "Temper repository root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if err := releaseartifact.ValidateVersion(*version); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("--output is required")
	}
	repoPath, err := validatedRepo(*repo)
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(outputPath), ".temper-build-*")
	if err != nil {
		return fmt.Errorf("create temporary build path: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary build path: %w", err)
	}
	defer os.Remove(temporaryPath)

	linkerFlags := "-s -w -buildid= -X main.version=" + *version
	command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags", linkerFlags, "-o", temporaryPath, "./cmd/temper")
	command.Dir = repoPath
	command.Env = targetEnvironment(os.Environ())
	var commandOutput bytes.Buffer
	command.Stdout = &commandOutput
	command.Stderr = &commandOutput
	if err := command.Run(); err != nil {
		return fmt.Errorf("go build: %w: %s", err, strings.TrimSpace(commandOutput.String()))
	}
	if err := validateBinary(temporaryPath); err != nil {
		return err
	}
	binary, err := os.ReadFile(temporaryPath)
	if err != nil {
		return fmt.Errorf("read built binary: %w", err)
	}
	unchanged, err := commitFile(outputPath, binary, 0o755)
	if err != nil {
		return err
	}
	state := "created"
	if unchanged {
		state = "unchanged"
	}
	fmt.Fprintf(stdout, "RESULT release-build state=%s version=%s target=%s/%s output=%s\n", state, *version, releaseartifact.TargetOS, releaseartifact.TargetArch, outputPath)
	return nil
}

func runPackage(arguments []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("package", flag.ContinueOnError)
	flags.SetOutput(stderr)
	version := flags.String("version", "", "release version without leading v")
	binaryPathFlag := flags.String("binary", "", "signed release binary path")
	output := flags.String("output", "", "output asset directory")
	repo := flags.String("repo", ".", "Temper repository root")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if err := releaseartifact.ValidateVersion(*version); err != nil {
		return err
	}
	if *binaryPathFlag == "" || *output == "" {
		return fmt.Errorf("--binary and --output are required")
	}
	repoPath, err := validatedRepo(*repo)
	if err != nil {
		return err
	}
	binaryPath, err := filepath.Abs(*binaryPathFlag)
	if err != nil {
		return fmt.Errorf("resolve binary path: %w", err)
	}
	if err := validateBinary(binaryPath); err != nil {
		return err
	}
	if err := validateBinaryVersion(binaryPath, *version); err != nil {
		return err
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		return fmt.Errorf("read binary: %w", err)
	}
	license, err := os.ReadFile(filepath.Join(repoPath, "LICENSE"))
	if err != nil {
		return fmt.Errorf("read Temper LICENSE: %w", err)
	}
	modules, err := listLinkedModules(repoPath)
	if err != nil {
		return err
	}
	noticeModules, err := readModuleNotices(modules)
	if err != nil {
		return err
	}
	notices, err := releaseartifact.BuildNotices(noticeModules)
	if err != nil {
		return fmt.Errorf("build third-party notices: %w", err)
	}
	archive, identity, checksum, err := releaseartifact.BuildArchive(*version, []releaseartifact.File{
		{Name: "temper", Data: binary},
		{Name: "LICENSE", Data: license},
		{Name: "THIRD_PARTY_NOTICES.txt", Data: notices},
	})
	if err != nil {
		return fmt.Errorf("build release archive: %w", err)
	}
	archiveName, err := releaseartifact.ArchiveName(*version)
	if err != nil {
		return err
	}
	outputPath, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("resolve output directory: %w", err)
	}
	if err := os.MkdirAll(outputPath, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	archivePath := filepath.Join(outputPath, archiveName)
	checksumPath := filepath.Join(outputPath, archiveName+".sha256")
	if _, decided, err := compareExisting(archivePath, archive, 0o644); decided && err != nil {
		return err
	}
	if _, decided, err := compareExisting(checksumPath, checksum, 0o644); decided && err != nil {
		return err
	}
	archiveUnchanged, err := commitFile(archivePath, archive, 0o644)
	if err != nil {
		return err
	}
	checksumUnchanged, err := commitFile(checksumPath, checksum, 0o644)
	if err != nil {
		return err
	}
	state := "created"
	if archiveUnchanged && checksumUnchanged {
		state = "unchanged"
	}
	fmt.Fprintf(stdout, "RESULT release-package state=%s version=%s archive=%s sha256=%s modules=%d\n", state, *version, archivePath, identity, len(modules))
	return nil
}

func validatedRepo(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve repository path: %w", err)
	}
	for _, required := range []string{"go.mod", "LICENSE", filepath.Join("cmd", "temper", "main.go")} {
		info, statErr := os.Stat(filepath.Join(absolute, required))
		if statErr != nil {
			return "", fmt.Errorf("validate repository %q: %s: %w", absolute, required, statErr)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("validate repository %q: %s is not a regular file", absolute, required)
		}
	}
	return absolute, nil
}

func targetEnvironment(environ []string) []string {
	result := make([]string, 0, len(environ)+7)
	for _, entry := range environ {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK", "GOENV", "GOTOOLCHAIN", "SOURCE_DATE_EPOCH":
			continue
		default:
			result = append(result, entry)
		}
	}
	return append(result,
		"GOOS="+releaseartifact.TargetOS,
		"GOARCH="+releaseartifact.TargetArch,
		"CGO_ENABLED=0",
		"GOFLAGS=",
		"GOWORK=off",
		"GOENV=off",
		"GOTOOLCHAIN=local",
		"SOURCE_DATE_EPOCH=0",
	)
}

func validateBinary(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("binary %q is not a regular file", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("binary %q is not executable", path)
	}
	file, err := macho.Open(path)
	if err != nil {
		return fmt.Errorf("binary %q is not Mach-O: %w", path, err)
	}
	defer file.Close()
	if file.Cpu != macho.CpuArm64 {
		return fmt.Errorf("binary %q target is %s, want arm64", path, file.Cpu)
	}
	return nil
}

func validateBinaryVersion(path, version string) error {
	command := exec.Command(path, "version")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read binary version: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if got, want := string(output), "temper "+version+"\n"; got != want {
		return fmt.Errorf("binary version output is %q, want %q", got, want)
	}
	return nil
}

type listedPackage struct {
	Standard bool          `json:"Standard"`
	Module   *listedModule `json:"Module"`
}

type listedModule struct {
	Path    string        `json:"Path"`
	Version string        `json:"Version"`
	Dir     string        `json:"Dir"`
	Main    bool          `json:"Main"`
	Replace *listedModule `json:"Replace"`
}

type linkedModule struct {
	Path    string
	Version string
	Dir     string
}

func listLinkedModules(repoPath string) ([]linkedModule, error) {
	command := exec.Command("go", "list", "-deps", "-json", "./cmd/temper")
	command.Dir = repoPath
	command.Env = targetEnvironment(os.Environ())
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("list linked modules: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	modules, err := modulesFromList(&stdout)
	if err != nil {
		return nil, fmt.Errorf("parse linked modules: %w", err)
	}
	return modules, nil
}

func modulesFromList(reader io.Reader) ([]linkedModule, error) {
	decoder := json.NewDecoder(reader)
	byIdentity := make(map[string]linkedModule)
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if pkg.Standard || pkg.Module == nil || pkg.Module.Main {
			continue
		}
		module := pkg.Module
		if module.Replace != nil {
			return nil, fmt.Errorf("module replacement is not allowed in a public release: %s", module.Path)
		}
		if module.Path == "" || module.Version == "" || module.Dir == "" {
			return nil, fmt.Errorf("linked module has incomplete identity: path=%q version=%q dir=%q", module.Path, module.Version, module.Dir)
		}
		identity := module.Path + "@" + module.Version
		candidate := linkedModule{Path: module.Path, Version: module.Version, Dir: module.Dir}
		if existing, ok := byIdentity[identity]; ok && existing.Dir != candidate.Dir {
			return nil, fmt.Errorf("linked module %s has conflicting source directories", identity)
		}
		byIdentity[identity] = candidate
	}
	if len(byIdentity) == 0 {
		return nil, fmt.Errorf("linked module graph contains no third-party modules")
	}
	modules := make([]linkedModule, 0, len(byIdentity))
	for _, module := range byIdentity {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Path == modules[j].Path {
			return modules[i].Version < modules[j].Version
		}
		return modules[i].Path < modules[j].Path
	})
	return modules, nil
}

func readModuleNotices(modules []linkedModule) ([]releaseartifact.ModuleNotice, error) {
	result := make([]releaseartifact.ModuleNotice, 0, len(modules))
	for _, module := range modules {
		entries, err := os.ReadDir(module.Dir)
		if err != nil {
			return nil, fmt.Errorf("read module root %s@%s: %w", module.Path, module.Version, err)
		}
		var files []releaseartifact.NoticeFile
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !isNoticeFilename(entry.Name()) {
				continue
			}
			path := filepath.Join(module.Dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return nil, fmt.Errorf("inspect %s: %w", path, err)
			}
			if info.Size() <= 0 || info.Size() > maxNoticeFileBytes {
				return nil, fmt.Errorf("module notice %s has invalid size %d", path, info.Size())
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read module notice %s: %w", path, err)
			}
			files = append(files, releaseartifact.NoticeFile{Name: entry.Name(), Data: data})
		}
		if len(files) == 0 {
			return nil, fmt.Errorf("linked module %s@%s has no regular root LICENSE, COPYING, or NOTICE file", module.Path, module.Version)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		result = append(result, releaseartifact.ModuleNotice{Path: module.Path, Version: module.Version, Files: files})
	}
	return result, nil
}

func isNoticeFilename(name string) bool {
	upper := strings.ToUpper(name)
	return strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE")
}

// commitFile atomically publishes new bytes, returns true for an exact
// second-run match, and refuses to reuse the destination for different bytes
// or modes.
func commitFile(path string, data []byte, mode fs.FileMode) (bool, error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return false, fmt.Errorf("create destination directory: %w", err)
	}
	if unchanged, decided, err := compareExisting(path, data, mode); decided {
		return unchanged, err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, fmt.Errorf("create temporary artifact: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("set temporary artifact mode: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("write temporary artifact: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("sync temporary artifact: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close temporary artifact: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			if unchanged, decided, compareErr := compareExisting(path, data, mode); decided {
				return unchanged, compareErr
			}
		}
		return false, fmt.Errorf("publish artifact %q: %w", path, err)
	}
	return false, nil
}

func compareExisting(path string, data []byte, mode fs.FileMode) (unchanged, decided bool, err error) {
	info, statErr := os.Lstat(path)
	if errors.Is(statErr, fs.ErrNotExist) {
		return false, false, nil
	}
	if statErr != nil {
		return false, true, fmt.Errorf("inspect existing artifact %q: %w", path, statErr)
	}
	if !info.Mode().IsRegular() {
		return false, true, fmt.Errorf("existing artifact %q is not a regular file", path)
	}
	existing, readErr := os.ReadFile(path)
	if readErr != nil {
		return false, true, fmt.Errorf("read existing artifact %q: %w", path, readErr)
	}
	if !bytes.Equal(existing, data) || info.Mode().Perm() != mode.Perm() {
		return false, true, fmt.Errorf("refusing to reuse artifact %q for different bytes or mode", path)
	}
	return true, true, nil
}
