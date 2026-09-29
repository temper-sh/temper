// Package managed owns explicit persistent availability, separately from the
// fixed-process experiment supervisor. llama-swap owns demand loading and TTL.
package managed

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/preset"
	"github.com/temper-sh/temper/internal/render"
	"github.com/temper-sh/temper/internal/render/engine"
	"github.com/temper-sh/temper/internal/runtimeconfig"
	"github.com/temper-sh/temper/internal/setup"
	"gopkg.in/yaml.v3"
)

type Process struct {
	Role      string   `json:"role"`
	Path      string   `json:"path"`
	Arguments []string `json:"arguments"`
	Record    string   `json:"record,omitempty"`
}
type Job struct {
	Layout         string    `json:"layout"`
	Name           string    `json:"name"`
	Generation     string    `json:"generation"`
	Label          string    `json:"label"`
	Listen         string    `json:"listen"`
	Router         string    `json:"router"`
	Config         string    `json:"config"`
	Processes      []Process `json:"processes"`
	Presets        []string  `json:"presets"`
	Launcher       string    `json:"launcher"`
	LauncherSHA256 string    `json:"launcher_sha256"`
}
type Rendered struct {
	Job    Job
	Config []byte
}
type ResolveExecutable func(catalog.Lock, string, string) (string, error)
type Launcher struct{ Path, SHA256 string }

// Render composes fully rendered presets, binding each command to its own exact
// executable. The router is shared only when its closure identity agrees.
func Render(root, listen string, c setup.Configuration, l setup.LayoutPlan, resolve ResolveExecutable, launcher Launcher) (Rendered, error) {
	if err := c.Validate(); err != nil {
		return Rendered{}, err
	}
	if len(l.Layout.Presets) == 0 {
		return Rendered{}, errors.New("empty layout has no service to activate")
	}
	models := map[string]any{}
	groups := map[string]any{}
	var processes []Process
	router, routerIdentity := "", ""
	ids := slices.Clone(l.Layout.Presets)
	slices.Sort(ids)
	for _, id := range ids {
		selected := c.Presets[id]
		lock := selected.Lock
		for _, e := range lock.Records.Engines {
			if e.Family != engine.LlamaServer && e.Family != engine.Splash {
				return Rendered{}, fmt.Errorf("managed layouts do not yet support %s process topology; use its explicit execution contract", e.Family)
			}
		}
		sets, err := preset.Software(lock)
		if err != nil {
			return Rendered{}, err
		}
		for _, set := range sets {
			if set.Package == "llama-swap" {
				if routerIdentity != "" && routerIdentity != set.ID {
					return Rendered{}, errors.New("layout presets require different router closures; explicitly resolve them to one reviewed router")
				}
				routerIdentity = set.ID
			}
		}
		path, err := resolve(lock, "llama-swap", "llama-swap")
		if err != nil {
			return Rendered{}, err
		}
		router = path
		bundle, err := preset.Render(root, lock)
		if err != nil {
			return Rendered{}, err
		}
		var document map[string]any
		var requirements runtimeconfig.Document
		for _, a := range bundle.Artifacts {
			switch a.Path {
			case "llama-swap/config.yaml":
				err = yaml.Unmarshal(a.Data, &document)
			case "runtime/requirements.json":
				requirements, err = runtimeconfig.Parse(a.Data)
			}
			if err != nil {
				return Rendered{}, err
			}
		}
		p, err := lock.Projections()
		if err != nil {
			return Rendered{}, err
		}
		commands, err := render.Commands(render.Inputs{Root: root, Mode: lock.Preset, Manifest: p.Manifest, Lock: p.Artifacts})
		if err != nil {
			return Rendered{}, err
		}
		for original, command := range commands {
			runtime := command.Runtime()
			executable, err := resolve(lock, runtime.Requirement.Package, runtime.Requirement.RelativeExecutable)
			if err != nil {
				return Rendered{}, err
			}
			process := Process{Role: id, Path: executable, Arguments: command.Arguments()}
			_, launchKey := launchBytes(process, launcher.SHA256)
			process.Record = filepath.Join(root, "managed", "commands", launchKey, "process.json")
			row := document["models"].(map[string]any)[original].(map[string]any)
			row["cmd"] = launchCommand(launcher.Path, process.Record)
			row["name"] = selected.Name
			row["ttl"] = l.Layout.IdleSeconds
			if original != id {
				row["useModelName"] = original
			}
			if l.Layout.Default == id && id != "default" {
				if slices.Contains(ids, "default") {
					return Rendered{}, errors.New("preset ID default conflicts with the selected default route")
				}
				row["aliases"] = []string{"default"}
			}
			models[id] = row
			processes = append(processes, process)
			for _, r := range requirements.Requirements {
				if r.Package == "llama-swap" || r.RelativeExecutable == runtime.Requirement.RelativeExecutable {
					continue
				}
				path, err := resolve(lock, r.Package, r.RelativeExecutable)
				if err != nil {
					return Rendered{}, err
				}
				processes = append(processes, Process{Role: id + "/" + r.Role, Path: path, Arguments: r.Arguments})
			}
		}
	}
	if l.AllFit {
		groups["selected"] = map[string]any{"members": ids, "swap": false, "exclusive": false, "persistent": false}
	} else {
		if len(l.Layout.Startup) > 0 {
			groups["startup"] = map[string]any{"members": l.Layout.Startup, "swap": false, "exclusive": true, "persistent": false}
		}
		for _, id := range ids {
			if !slices.Contains(l.Layout.Startup, id) {
				groups[id] = map[string]any{"members": []string{id}, "swap": false, "exclusive": true, "persistent": false}
			}
		}
	}
	doc := map[string]any{"healthCheckTimeout": 660, "startPort": 10001, "globalTTL": l.Layout.IdleSeconds, "logToStdout": "both", "models": models, "routing": map[string]any{"router": map[string]any{"use": "group", "settings": map[string]any{"groups": groups}}}}
	if len(l.Layout.Startup) > 0 {
		doc["hooks"] = map[string]any{"on_startup": map[string]any{"preload": l.Layout.Startup}}
	}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		return Rendered{}, err
	}
	job := Job{Layout: l.ID, Name: l.Layout.Name, Label: jobLabel(root), Listen: listen, Router: router, Processes: processes, Presets: ids, Launcher: launcher.Path, LauncherSHA256: launcher.SHA256}
	job.Generation = job.digest(raw)
	job.Config = filepath.Join(root, "managed", "generations", job.Generation, "config.yaml")
	if err := job.Validate(root); err != nil {
		return Rendered{}, err
	}
	return Rendered{Job: job, Config: raw}, nil
}

func jobLabel(root string) string {
	hash := sha256.Sum256([]byte(root))
	return "sh.temper." + hex.EncodeToString(hash[:12])
}

func (j Job) digest(raw []byte) string {
	identity, _ := json.Marshal(struct {
		Config, Router, Listen, Launcher, LauncherSHA string
		Processes                                     []Process
	}{string(raw), j.Router, j.Listen, j.Launcher, j.LauncherSHA256, j.Processes})
	hash := sha256.Sum256(identity)
	return hex.EncodeToString(hash[:])
}

var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (j Job) Arguments() []string {
	return []string{"--config", j.Config, "--listen", j.Listen, "--watch-config=false"}
}
func (j Job) Validate(root string) error {
	if j.Label != jobLabel(root) || !filepath.IsAbs(j.Router) || !filepath.IsAbs(j.Launcher) || !digestPattern.MatchString(j.LauncherSHA256) || !digestPattern.MatchString(j.Generation) || j.Config != filepath.Join(root, "managed", "generations", j.Generation, "config.yaml") {
		return fmt.Errorf("invalid managed job identity")
	}
	host, port, err := net.SplitHostPort(j.Listen)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || host != "127.0.0.1" || n < 1024 || n > 65535 {
		return errors.New("invalid managed listener")
	}
	for _, p := range j.Processes {
		if p.Role == "" || !filepath.IsAbs(p.Path) || strings.ContainsAny(p.Path, "\x00\r\n") {
			return errors.New("invalid managed process")
		}
		for _, arg := range p.Arguments {
			if strings.ContainsRune(arg, '\x00') {
				return errors.New("invalid managed argument")
			}
		}
		if p.Record != "" {
			_, key := launchBytes(p, j.LauncherSHA256)
			if p.Record != filepath.Join(root, "managed", "commands", key, "process.json") {
				return errors.New("invalid managed launch record path")
			}
		}
	}
	return nil
}

func verifyJobConfig(j Job) error {
	info, err := os.Lstat(j.Config)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return errors.New("managed configuration must be a bounded regular file")
	}
	raw, err := os.ReadFile(j.Config)
	if err != nil {
		return err
	}
	if j.digest(raw) != j.Generation {
		return errors.New("managed configuration differs from its generation")
	}
	return nil
}
