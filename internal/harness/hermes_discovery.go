package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var hermesProfileName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func discoverHermes(home string, getenv func(string) string, lookPath func(string) (string, error)) Option {
	_, executableErr := lookPath("hermes")
	o := Option{Selection: Selection{ID: "hermes"}, Name: "Hermes", Detected: executableErr == nil,
		Detail: "Selected profile configuration. Automatic model discovery stays enabled."}
	dir, err := hermesDirectory(home, getenv)
	if err != nil {
		o.Problem = "Cannot resolve Hermes profile: " + err.Error()
		return o
	}
	o.ModelsFile = filepath.Join(dir, "config.yaml")
	resolved, err := canonical(o.ModelsFile)
	if err != nil {
		o.Problem = "Cannot inspect " + o.ModelsFile + ": " + err.Error()
	} else {
		o.ModelsFile = resolved
	}
	return o
}

// Follow a normal Hermes CLI invocation: a pinned profile wins; a root home
// still follows active_profile. Nothing here creates or launches a profile.
func hermesDirectory(home string, getenv func(string) string) (string, error) {
	native := filepath.Join(home, ".hermes")
	dir := native
	if value := strings.TrimSpace(getenv("HERMES_HOME")); value != "" {
		var missing string
		value = os.Expand(value, func(key string) string {
			if key == "HOME" {
				return home
			}
			v := getenv(key)
			if v == "" {
				missing = key
			}
			return v
		})
		if missing != "" {
			return "", fmt.Errorf("HERMES_HOME references unset or empty %s", missing)
		}
		if value == "~" {
			value = home
		} else if strings.HasPrefix(value, "~/") {
			value = filepath.Join(home, value[2:])
		} else if strings.HasPrefix(value, "~") {
			return "", fmt.Errorf("use an absolute HERMES_HOME instead of %q", value)
		}
		var err error
		dir, err = filepath.Abs(value)
		if err != nil {
			return "", err
		}
	}
	if filepath.Base(filepath.Dir(dir)) == "profiles" {
		return dir, nil
	}
	// Hermes reads the sticky selection from the installation root, but resolves
	// a selected name under the explicit HERMES_HOME when one was supplied.
	stickyRoot := dir
	resolved, err := canonical(dir)
	if err != nil {
		return "", err
	}
	nativeResolved, err := canonical(native)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(nativeResolved, resolved); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		stickyRoot = native
	}
	raw, _, err := read(filepath.Join(stickyRoot, "active_profile"))
	if err != nil {
		return "", err
	}
	name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(raw), "\ufeff")))
	if name == "" || name == "default" {
		return dir, nil
	}
	if !hermesProfileName.MatchString(name) {
		return "", fmt.Errorf("invalid active profile %q", name)
	}
	profile := filepath.Join(dir, "profiles", name)
	if _, err := os.Stat(filepath.Join(dir, "profiles", ".deleted", name)); err == nil {
		return "", fmt.Errorf("active profile %q was deleted", name)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	for _, marker := range []string{"config.yaml", ".env", "SOUL.md", "profile.yaml", "auth.json", "state.db"} {
		if info, err := os.Lstat(filepath.Join(profile, marker)); err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return profile, nil
		}
	}
	return "", fmt.Errorf("active profile %q has no profile configuration; select an existing Hermes profile", name)
}
