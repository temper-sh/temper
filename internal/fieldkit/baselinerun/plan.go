package baselinerun

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/temper-sh/temper/internal/fieldkit/baseline"
	"github.com/temper-sh/temper/internal/fieldkit/catalog"
)

var targetTokenPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._+-][a-z0-9]+)*$`)

type Invocation struct {
	StageID    string
	Path       string
	Arguments  []string
	OutputPath string
	NoProcess  bool
	Protocol   *ProtocolInvocation
}

type ProtocolInvocation struct {
	ID           string
	Revision     int
	Schema       string
	TemperPath   string
	Root         string
	SoftwareLock string
	Generation   string
	Installation string
	Model        string
	Listen       string
	Report       string
	LogDirectory string
}

func Plan(document Document, promoted baseline.Entry) (Invocation, error) {
	_, stage, ok := document.Next()
	if !ok {
		return Invocation{}, errors.New("baseline session has no pending stage")
	}
	root := document.Workspace.Root
	manifest := filepath.Join(document.Workspace.PackageRoot, filepath.FromSlash(promoted.Package.Mechanics.Manifest.Path))
	manifestLock := filepath.Join(document.Workspace.PackageRoot, filepath.FromSlash(promoted.Package.Mechanics.ManifestLock.Path))
	softwareLock := document.Workspace.SoftwareLock.Path
	installation := promoted.Package.Mechanics.Installation
	evidenceRoot := filepath.Join(root, "field-kit")
	invocation := Invocation{StageID: stage.ID, Path: document.Temper.Path, OutputPath: filepath.Join(evidenceRoot, "stages", stage.ID+".stdout")}
	switch stage.Operation {
	case "software-install":
		invocation.Arguments = []string{"software", "install", "--root", root, "--installation", installation, "--lock", softwareLock}
	case "model-fetch":
		invocation.Arguments = []string{"fetch", promoted.Package.Profile.Layout, "--root", root, "--manifest", manifest, "--lock", manifestLock}
	case "config-apply":
		invocation.Arguments = []string{"apply", "--root", root, "--manifest", manifest, "--lock", manifestLock, "--mode", promoted.Package.Mechanics.Mode}
	case "software-check":
		invocation.Arguments = []string{"software", "check", "--root", root, "--installation", installation, "--lock", softwareLock}
	case "artifact-check":
		invocation.Arguments = []string{"check", "--root", root, "--manifest", manifest, "--lock", manifestLock, "--mode", promoted.Package.Mechanics.Mode, "--verify"}
	case "material-bind":
		if document.Generation == "" {
			return Invocation{}, errors.New("material binding requires a completed config generation")
		}
		invocation.Arguments = []string{"field-kit", "bind", "--root", root, "--manifest-lock", manifestLock, "--generation", document.Generation, "--installation", installation + "=" + softwareLock}
		invocation.OutputPath = filepath.Join(evidenceRoot, "binding.yaml")
	case "live-protocol":
		if document.Generation == "" || document.Binding == nil {
			return Invocation{}, errors.New("live protocol requires exact apply and binding evidence")
		}
		if promoted.Package.Mechanics.Protocol == nil {
			return Invocation{}, errors.New("live protocol is not owned by this Temper release")
		}
		invocation.Path = ""
		invocation.Arguments = nil
		invocation.Protocol = &ProtocolInvocation{
			ID: promoted.Package.Mechanics.Protocol.ID, Revision: promoted.Package.Mechanics.Protocol.Revision,
			Schema: promoted.Package.Mechanics.Protocol.Schema, TemperPath: document.Temper.Path,
			Root: root, SoftwareLock: softwareLock, Generation: document.Generation, Installation: installation,
			Model: promoted.Package.Profile.Layout, Listen: "127.0.0.1:18080",
			Report: filepath.Join(evidenceRoot, "protocol-report.json"), LogDirectory: filepath.Join(evidenceRoot, "protocol"),
		}
	case "outcome":
		if document.Outcome == "keep" {
			invocation.NoProcess = true
			invocation.Path = ""
			invocation.Arguments = nil
			break
		}
		invocation.Arguments = []string{"software", "remove", "--root", root, "--installation", installation, "--lock", softwareLock}
	default:
		return Invocation{}, fmt.Errorf("baseline stage operation %q is unsupported", stage.Operation)
	}
	return invocation, nil
}

func CompileSoftwareLock(promoted baseline.Package, facts catalog.MachineFacts) ([]byte, error) {
	if err := promoted.Software.Validate(); err != nil {
		return nil, err
	}
	targets := []string{facts.Target.OS, facts.Target.Arch, facts.Target.Distribution}
	if facts.Target.DistributionVersion != "" {
		targets = append(targets, facts.Target.DistributionVersion)
	}
	for _, value := range targets {
		if !targetTokenPattern.MatchString(value) {
			return nil, fmt.Errorf("machine target token %q cannot be compiled", value)
		}
	}
	var output bytes.Buffer
	fmt.Fprintln(&output, "schema: temper-software-lock/v1")
	fmt.Fprintln(&output, "provenance:")
	fmt.Fprintln(&output, "  experiment:")
	fmt.Fprintf(&output, "    schema: %s\n", promoted.Software.DefinitionSchema)
	fmt.Fprintf(&output, "    id: %s\n", promoted.Software.DefinitionID)
	fmt.Fprintf(&output, "    definition_sha256: %s\n", promoted.Software.DefinitionSHA256)
	fmt.Fprintln(&output, "requires: []")
	fmt.Fprintln(&output, "target:")
	fmt.Fprintf(&output, "  os: %s\n  arch: %s\n  distribution: %s\n", facts.Target.OS, facts.Target.Arch, facts.Target.Distribution)
	if facts.Target.DistributionVersion != "" {
		fmt.Fprintf(&output, "  distribution_version: %s\n", facts.Target.DistributionVersion)
	}
	fmt.Fprintf(&output, "resolved: %s\n", promoted.Software.Resolved)
	fmt.Fprintln(&output, "selections:")
	for _, item := range promoted.Software.Packages {
		fmt.Fprintf(&output, "  %s:\n", item.ID)
		fmt.Fprintln(&output, "    provenance: experiment")
		fmt.Fprintln(&output, "    method: release-artifact")
		fmt.Fprintln(&output, "    adapter: upstream-release")
		fmt.Fprintf(&output, "    recipe_revision: %s\n", item.RecipeRevision)
		fmt.Fprintf(&output, "    root_unit: upstream-release:%s:%s\n", item.Scope, item.ID)
	}
	fmt.Fprintln(&output, "units:")
	for _, item := range promoted.Software.Packages {
		fmt.Fprintf(&output, "  upstream-release:%s:%s:\n", item.Scope, item.ID)
		fmt.Fprintln(&output, "    adapter: upstream-release")
		fmt.Fprintf(&output, "    scope: %s\n", item.Scope)
		fmt.Fprintf(&output, "    native_name: %s\n", item.NativeName)
		fmt.Fprintf(&output, "    version: %s\n", item.Version)
		fmt.Fprintf(&output, "    revision: %s\n", item.Revision)
		fmt.Fprintln(&output, "    dependencies: []")
		fmt.Fprintln(&output, "    artifacts:")
		fmt.Fprintf(&output, "      - locator: %s\n", item.Locator)
		fmt.Fprintf(&output, "        sha256: %s\n", item.SHA256)
		fmt.Fprintf(&output, "        size: %d\n", item.Bytes)
		fmt.Fprintf(&output, "        unpacked_size: %d\n", item.UnpackedBytes)
		fmt.Fprintf(&output, "        installed_entries: %d\n", item.InstalledEntries)
		fmt.Fprintln(&output, "        format: tar.gz")
		fmt.Fprintf(&output, "        archive_root: %s\n", item.ArchiveRoot)
	}
	return output.Bytes(), nil
}

func ParseApplyGeneration(data []byte) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "RESULT apply ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			if generation, found := strings.CutPrefix(field, "generation="); found && sha256Pattern.MatchString(generation) {
				return generation, nil
			}
		}
	}
	return "", errors.New("Temper apply output has no exact generation")
}
