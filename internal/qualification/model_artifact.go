package qualification

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"strings"
)

var (
	artifactRepositoryPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	artifactRevisionPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ModelArtifactProfile pins every selected model byte and the metadata needed
// to interpret those bytes. It makes no runtime-quality claim by itself.
type ModelArtifactProfile struct {
	ProfileEnvelope `yaml:",inline"`
	Spec            ModelArtifactSpec `yaml:"spec"`
}

type ModelArtifactSpec struct {
	Source                ModelArtifactSource       `yaml:"source"`
	Files                 []ModelArtifactFile       `yaml:"files"`
	ModelFamily           string                    `yaml:"model_family"`
	Format                string                    `yaml:"format"`
	Quantization          ModelArtifactQuantization `yaml:"quantization"`
	Tokenizer             ArtifactComponent         `yaml:"tokenizer"`
	Template              ArtifactComponent         `yaml:"template"`
	Sidecars              []string                  `yaml:"sidecars"`
	DeclaredDownloadBytes int64                     `yaml:"declared_download_bytes"`
	License               ModelArtifactLicense      `yaml:"license"`
}

type ModelArtifactSource struct {
	Kind       string `yaml:"kind"`
	Repository string `yaml:"repository"`
	Revision   string `yaml:"revision"`
}

type ModelArtifactFile struct {
	Path    string `yaml:"path"`
	SHA256  string `yaml:"sha256"`
	Size    int64  `yaml:"size"`
	Purpose string `yaml:"purpose"`
}

type ModelArtifactQuantization struct {
	Family           string                     `yaml:"family"`
	RecipeRevision   string                     `yaml:"recipe_revision"`
	TensorAllocation []ModelArtifactTensorClass `yaml:"tensor_allocation"`
	Calibration      ArtifactCalibration        `yaml:"calibration"`
}

// ModelArtifactTensorClass gives a default storage precision plus any named
// tensor-class overrides. A required default row makes the allocation total.
type ModelArtifactTensorClass struct {
	TensorClass string `yaml:"tensor_class"`
	Precision   string `yaml:"precision"`
}

type ArtifactCalibration struct {
	State  string             `yaml:"state"`
	Source *MaterialReference `yaml:"source,omitempty"`
}

// ArtifactComponent names the exact selected file containing a component, or
// records that the component is not applicable. Embedded metadata names its
// containing weights file.
type ArtifactComponent struct {
	State string `yaml:"state"`
	Path  string `yaml:"path,omitempty"`
}

type ModelArtifactLicense struct {
	ID             string                     `yaml:"id"`
	Source         ModelArtifactLicenseSource `yaml:"source"`
	Redistribution string                     `yaml:"redistribution"`
}

type ModelArtifactLicenseSource struct {
	Repository string `yaml:"repository"`
	Revision   string `yaml:"revision"`
	Path       string `yaml:"path"`
}

// ParseModelArtifactProfile accepts only canonical model-artifact YAML.
func ParseModelArtifactProfile(data []byte) (ModelArtifactProfile, error) {
	return decodeCanonicalProfile(data, "model artifact profile", MarshalModelArtifactProfile)
}

// MarshalModelArtifactProfile validates a model artifact and returns canonical
// YAML.
func MarshalModelArtifactProfile(profile ModelArtifactProfile) ([]byte, error) {
	return encodeCanonicalProfile(profile, "model artifact profile", ModelArtifactProfile.Validate)
}

// Validate enforces the v1 common envelope and model-artifact body.
func (p ModelArtifactProfile) Validate() error {
	var problems []string
	problem := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	validateProfileEnvelope(p.ProfileEnvelope, ModelArtifactSchemaV1, problem)
	if len(p.Dependencies) != 0 {
		problem("model artifact dependencies must be empty")
	}
	validateModelArtifactEvidenceScopes(p, problem)
	validateModelArtifactSpec(p.Spec, problem)

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateModelArtifactEvidenceScopes(profile ModelArtifactProfile, problem func(string, ...any)) {
	for index, evidence := range profile.Evidence {
		if !scopeReferenceEqualsReference(evidence.Scope.ArtifactProfile, Reference{
			Schema: profile.Schema, ID: profile.ID, Revision: profile.Revision,
		}) {
			problem("evidence[%d].scope.artifact_profile must identify the containing model artifact", index)
		}
	}
}

func validateModelArtifactSpec(spec ModelArtifactSpec, problem func(string, ...any)) {
	if spec.Source.Kind != "hugging-face" && spec.Source.Kind != "upstream-release" {
		problem("spec.source.kind %q is not supported", spec.Source.Kind)
	}
	validateArtifactRepository("spec.source.repository", spec.Source.Repository, problem)
	validateArtifactRevision("spec.source.revision", spec.Source.Revision, problem)

	if len(spec.Files) == 0 {
		problem("spec.files must not be empty")
	}
	files := map[string]ModelArtifactFile{}
	previousPath := ""
	weightFiles := 0
	var totalBytes int64
	for index, file := range spec.Files {
		location := fmt.Sprintf("spec.files[%d]", index)
		if !safeArtifactPath(file.Path) {
			problem("%s.path %q is not a safe canonical relative path", location, file.Path)
		}
		if index > 0 && file.Path <= previousPath {
			problem("spec.files must be unique and sorted by path")
		}
		previousPath = file.Path
		files[file.Path] = file
		if !sha256Pattern.MatchString(file.SHA256) {
			problem("%s.sha256 must be 64 lowercase hexadecimal characters", location)
		}
		if file.Size <= 0 {
			problem("%s.size must be greater than zero", location)
		} else if totalBytes > math.MaxInt64-file.Size {
			problem("spec.files total size overflows int64")
		} else {
			totalBytes += file.Size
		}
		switch file.Purpose {
		case "weights":
			weightFiles++
		case "drafter", "other", "projector", "template", "tokenizer":
		default:
			problem("%s.purpose %q is not supported", location, file.Purpose)
		}
	}
	if weightFiles == 0 {
		problem("spec.files must include at least one weights file")
	}
	if spec.DeclaredDownloadBytes <= 0 || spec.DeclaredDownloadBytes != totalBytes {
		problem("spec.declared_download_bytes is %d, want selected-file sum %d", spec.DeclaredDownloadBytes, totalBytes)
	}

	if !stableIDPattern.MatchString(spec.ModelFamily) {
		problem("spec.model_family %q is not a lowercase stable id", spec.ModelFamily)
	}
	if spec.Format != "gguf" && spec.Format != "mlx-safetensors" && spec.Format != "safetensors" {
		problem("spec.format %q is not supported", spec.Format)
	}
	validateQuantization(spec.Quantization, problem)
	validateArtifactComponent("spec.tokenizer", spec.Tokenizer, false, "tokenizer", files, problem)
	validateArtifactComponent("spec.template", spec.Template, true, "template", files, problem)
	validateComponentFiles(spec, problem)
	validateSidecars(spec.Sidecars, files, problem)
	validateArtifactLicense(spec.License, problem)
}

func validateQuantization(quantization ModelArtifactQuantization, problem func(string, ...any)) {
	if !stableIDPattern.MatchString(quantization.Family) {
		problem("spec.quantization.family %q is not a lowercase stable id", quantization.Family)
	}
	if !exactRevisionIDPattern.MatchString(quantization.RecipeRevision) {
		problem("spec.quantization.recipe_revision %q is not an exact recipe id", quantization.RecipeRevision)
	}
	if len(quantization.TensorAllocation) == 0 {
		problem("spec.quantization.tensor_allocation must not be empty")
	}
	previous := ""
	hasDefault := false
	for index, allocation := range quantization.TensorAllocation {
		location := fmt.Sprintf("spec.quantization.tensor_allocation[%d]", index)
		if !stableIDPattern.MatchString(allocation.TensorClass) {
			problem("%s.tensor_class %q is not a lowercase stable id", location, allocation.TensorClass)
		}
		if !stableIDPattern.MatchString(allocation.Precision) {
			problem("%s.precision %q is not a lowercase stable id", location, allocation.Precision)
		}
		if index > 0 && allocation.TensorClass <= previous {
			problem("spec.quantization.tensor_allocation must be unique and sorted by tensor_class")
		}
		previous = allocation.TensorClass
		if allocation.TensorClass == "default" {
			hasDefault = true
		}
	}
	if !hasDefault {
		problem("spec.quantization.tensor_allocation must include a default tensor class")
	}

	switch quantization.Calibration.State {
	case "not-applicable":
		if quantization.Calibration.Source != nil {
			problem("spec.quantization.calibration source must be absent when state is not-applicable")
		}
	case "referenced":
		if quantization.Calibration.Source == nil {
			problem("spec.quantization.calibration source is required when state is referenced")
		} else {
			validateMaterialReference("spec.quantization.calibration.source", *quantization.Calibration.Source, problem)
		}
	default:
		problem("spec.quantization.calibration.state %q must be not-applicable or referenced", quantization.Calibration.State)
	}
}

func validateArtifactComponent(location string, component ArtifactComponent, allowNotApplicable bool, purpose string, files map[string]ModelArtifactFile, problem func(string, ...any)) {
	switch component.State {
	case "file":
		file, ok := files[component.Path]
		if !ok {
			problem("%s.path %q does not reference spec.files", location, component.Path)
		} else if file.Purpose != purpose && file.Purpose != "weights" {
			problem("%s.path %q references purpose %q, want %s or weights", location, component.Path, file.Purpose, purpose)
		}
		if !safeArtifactPath(component.Path) {
			problem("%s.path %q is not a safe canonical relative path", location, component.Path)
		}
	case "not-applicable":
		if !allowNotApplicable {
			problem("%s cannot be not-applicable", location)
		}
		if component.Path != "" {
			problem("%s.path must be absent when state is not-applicable", location)
		}
	default:
		allowed := "file"
		if allowNotApplicable {
			allowed += " or not-applicable"
		}
		problem("%s.state %q must be %s", location, component.State, allowed)
	}
}

func validateComponentFiles(spec ModelArtifactSpec, problem func(string, ...any)) {
	for _, file := range spec.Files {
		switch file.Purpose {
		case "tokenizer":
			if spec.Tokenizer.State != "file" || spec.Tokenizer.Path != file.Path {
				problem("spec.tokenizer must reference selected tokenizer file %q", file.Path)
			}
		case "template":
			if spec.Template.State != "file" || spec.Template.Path != file.Path {
				problem("spec.template must reference selected template file %q", file.Path)
			}
		}
	}
}

func validateSidecars(sidecars []string, files map[string]ModelArtifactFile, problem func(string, ...any)) {
	previous := ""
	listed := map[string]bool{}
	for index, sidecar := range sidecars {
		location := fmt.Sprintf("spec.sidecars[%d]", index)
		file, ok := files[sidecar]
		if !ok {
			problem("%s %q does not reference spec.files", location, sidecar)
		} else if file.Purpose != "drafter" && file.Purpose != "other" && file.Purpose != "projector" {
			problem("%s %q references non-sidecar purpose %q", location, sidecar, file.Purpose)
		}
		if index > 0 && sidecar <= previous {
			problem("spec.sidecars must be unique and sorted")
		}
		previous = sidecar
		listed[sidecar] = true
	}
	for filePath, file := range files {
		if (file.Purpose == "drafter" || file.Purpose == "other" || file.Purpose == "projector") && !listed[filePath] {
			problem("spec.sidecars must include %q with purpose %q", filePath, file.Purpose)
		}
	}
}

func validateArtifactLicense(license ModelArtifactLicense, problem func(string, ...any)) {
	if !stableIDPattern.MatchString(license.ID) {
		problem("spec.license.id %q is not a lowercase stable id", license.ID)
	}
	validateArtifactRepository("spec.license.source.repository", license.Source.Repository, problem)
	validateArtifactRevision("spec.license.source.revision", license.Source.Revision, problem)
	if !safeArtifactPath(license.Source.Path) {
		problem("spec.license.source.path %q is not a safe canonical relative path", license.Source.Path)
	}
	if license.Redistribution != "referenced-not-vendored" {
		problem("spec.license.redistribution must be referenced-not-vendored")
	}
}

func validateArtifactRepository(location, repository string, problem func(string, ...any)) {
	if !artifactRepositoryPattern.MatchString(repository) {
		problem("%s %q must be owner/name", location, repository)
	}
}

func validateArtifactRevision(location, revision string, problem func(string, ...any)) {
	if !artifactRevisionPattern.MatchString(revision) {
		problem("%s must be a 40-character lowercase commit hash", location)
	}
}

func safeArtifactPath(value string) bool {
	return value != "" && value != "." && value != ".." &&
		!strings.Contains(value, "\\") &&
		!strings.HasPrefix(value, "/") &&
		!strings.HasPrefix(value, "../") &&
		path.Clean(value) == value
}
