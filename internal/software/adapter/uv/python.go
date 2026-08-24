package uv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/temper-sh/temper/internal/software"
	"github.com/temper-sh/temper/internal/software/catalog"
	"github.com/temper-sh/temper/internal/software/policy"
	"github.com/temper-sh/temper/internal/software/version"
)

var (
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	buildPattern  = regexp.MustCompile(`^[0-9]{8}$`)
)

type pythonRuntime struct {
	Version  string
	Revision string
	Artifact software.Artifact
}

type pythonDownload struct {
	Name string `json:"name"`
	Arch struct {
		Family  string  `json:"family"`
		Variant *string `json:"variant"`
	} `json:"arch"`
	OS         string  `json:"os"`
	Libc       string  `json:"libc"`
	Major      int     `json:"major"`
	Minor      int     `json:"minor"`
	Patch      int     `json:"patch"`
	Prerelease string  `json:"prerelease"`
	URL        string  `json:"url"`
	SHA256     string  `json:"sha256"`
	Variant    *string `json:"variant"`
	Build      string  `json:"build"`
}

func selectPython(data []byte, recipe catalog.Recipe, constraints []string, target software.Target) (pythonRuntime, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var downloads map[string]pythonDownload
	if err := decoder.Decode(&downloads); err != nil {
		return pythonRuntime{}, fmt.Errorf("decode uv managed-Python metadata: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return pythonRuntime{}, errors.New("decode uv managed-Python metadata: trailing JSON value")
		}
		return pythonRuntime{}, fmt.Errorf("decode uv managed-Python metadata: %w", err)
	}
	if len(downloads) == 0 {
		return pythonRuntime{}, errors.New("uv managed-Python metadata is empty")
	}

	var selected *pythonRuntime
	for key, download := range downloads {
		if download.Name != "cpython" || download.OS != "darwin" || download.Arch.Family != "aarch64" || download.Libc != "none" || download.Arch.Variant != nil || download.Variant != nil {
			continue
		}
		candidate, err := validatePythonDownload(key, download, target)
		if err != nil {
			return pythonRuntime{}, err
		}
		matched, err := policy.Matches(recipe, candidate.Version, candidate.Revision, nil)
		if err != nil {
			return pythonRuntime{}, fmt.Errorf("select uv managed Python: %w", err)
		}
		if !matched {
			continue
		}
		for _, constraint := range constraints {
			matched, err = version.Satisfies("pep440", candidate.Version, constraint)
			if err != nil {
				return pythonRuntime{}, fmt.Errorf("select uv managed Python constraint %q: %w", constraint, err)
			}
			if !matched {
				break
			}
		}
		if !matched {
			continue
		}
		if selected == nil {
			copy := candidate
			selected = &copy
			continue
		}
		order, err := version.Compare("pep440", candidate.Version, selected.Version)
		if err != nil {
			return pythonRuntime{}, err
		}
		if order > 0 || (order == 0 && candidate.Revision > selected.Revision) {
			copy := candidate
			selected = &copy
		} else if order == 0 && candidate.Revision == selected.Revision && candidate.Artifact != selected.Artifact {
			return pythonRuntime{}, fmt.Errorf("uv managed-Python metadata has conflicting artifacts for %s %s", candidate.Version, candidate.Revision)
		}
	}
	if selected == nil {
		return pythonRuntime{}, errors.New("uv managed-Python metadata has no target runtime satisfying catalog policy")
	}
	return *selected, nil
}

func validatePythonDownload(key string, download pythonDownload, target software.Target) (pythonRuntime, error) {
	if target.OS != "darwin" || target.Arch != "arm64" {
		return pythonRuntime{}, errors.New("uv managed Python target must be darwin/arm64")
	}
	versionValue := fmt.Sprintf("%d.%d.%d%s", download.Major, download.Minor, download.Patch, download.Prerelease)
	if err := version.Validate("pep440", versionValue); err != nil {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry %q version: %w", key, err)
	}
	wantKey := "cpython-" + versionValue + "-darwin-aarch64-none"
	if key != wantKey {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry key %q does not match %q", key, wantKey)
	}
	if !buildPattern.MatchString(download.Build) {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry %q build is not an eight-digit release", key)
	}
	parsed, err := url.Parse(download.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry %q URL must be absolute credential-free HTTPS", key)
	}
	if parsed.Host != "github.com" && parsed.Host != "releases.astral.sh" {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry %q URL host %q is not approved", key, parsed.Host)
	}
	if !strings.Contains(parsed.Path, "/python-build-standalone/releases/download/"+download.Build+"/") {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry %q URL does not match build %q", key, download.Build)
	}
	if !sha256Pattern.MatchString(download.SHA256) {
		return pythonRuntime{}, fmt.Errorf("uv managed-Python entry %q SHA-256 is invalid", key)
	}
	return pythonRuntime{
		Version: versionValue, Revision: "python-build-standalone:" + download.Build,
		Artifact: software.Artifact{Locator: download.URL, SHA256: download.SHA256},
	}, nil
}
