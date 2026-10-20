// Package harness projects selected models and reconciles explicitly selected
// client configuration files. Discovery and preflight are reads; Publish writes.
package harness

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/modelmemory"
)

type Selection struct {
	ID string `json:"id"`
	// ModelsFile is the client's provider/model configuration: JSON for Pi/PiG,
	// config.yaml for Hermes. Persist the reviewed destination, not an env rule.
	ModelsFile string `json:"models_file"`
}

func Validate(selections []Selection) error {
	seen := map[string]bool{}
	for _, s := range selections {
		if s.ID != "pi" && s.ID != "pig" && s.ID != "hermes" {
			return fmt.Errorf("unsupported harness %q", s.ID)
		}
		if seen[s.ID] {
			return fmt.Errorf("harness %q selected more than once", s.ID)
		}
		seen[s.ID] = true
		if !filepath.IsAbs(s.ModelsFile) || filepath.Clean(s.ModelsFile) != s.ModelsFile || strings.ContainsAny(s.ModelsFile, "\x00\r\n\x1b") {
			return fmt.Errorf("harness %s requires an absolute, clean models-file path", s.ID)
		}
	}
	return nil
}

type Option struct {
	Selection
	Name     string
	Detected bool
	Detail   string
	Problem  string
}

// Discover follows the clients' directory rules without executing any client.
func Discover(home string, getenv func(string) string, lookPath func(string) (string, error)) ([]Option, error) {
	expand := func(path string) string {
		if path == "~" {
			return home
		}
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(home, path[2:])
		}
		return path
	}
	pi := getenv("PI_CODING_AGENT_DIR")
	if pi == "" {
		pi = filepath.Join(home, ".pi", "agent")
	}
	pig := getenv("PIG_CODING_AGENT_DIR")
	if getenv("PIG_USE_PI_DIRS") == "1" {
		pig = pi
	} else if pig == "" {
		base := getenv("PIG_HOME")
		if base == "" {
			if xdg := getenv("XDG_CONFIG_HOME"); xdg != "" {
				base = filepath.Join(expand(xdg), "pig")
			} else {
				base = filepath.Join(home, ".pig")
			}
		}
		pig = filepath.Join(expand(base), "agent")
	}
	var options []Option
	for _, s := range []struct{ id, name, dir string }{{"pi", "Pi", pi}, {"pig", "PiG", pig}} {
		path, err := filepath.Abs(filepath.Join(expand(s.dir), "models.json"))
		if err != nil {
			return nil, err
		}
		resolved, pathErr := canonical(path)
		if pathErr == nil {
			path = resolved
		}
		_, executableErr := lookPath(s.id)
		option := Option{Selection: Selection{ID: s.id, ModelsFile: path}, Name: s.name, Detected: executableErr == nil}
		if pathErr != nil {
			option.Problem = "Cannot inspect " + path + ": " + pathErr.Error()
		}
		options = append(options, option)
	}
	if options[0].ModelsFile == options[1].ModelsFile {
		for i := range options {
			options[i].Detail = "Shared models.json: these models will be visible to both Pi and PiG."
		}
	}
	options = append(options, discoverHermes(home, getenv, lookPath))
	return options, nil
}

// canonical resolves existing ancestors, including a file symlink, without
// creating absent directories. The reviewed destination can then be persisted.
func canonical(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	if _, e := os.Lstat(path); e == nil {
		return "", fmt.Errorf("dangling symlink at %s", path)
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	parent, err = canonical(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

type Preset struct {
	ID, Name string
	Lock     catalog.Lock
}

type Model struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	ContextWindow  int            `json:"contextWindow"`
	MaxTokens      int            `json:"maxTokens"`
	Input          []string       `json:"input"`
	Reasoning      bool           `json:"reasoning"`
	Cost           map[string]int `json:"cost"`
	Compat         map[string]any `json:"compat"`
	SamplingParams map[string]any `json:"samplingParams,omitempty"`
}

// Models uses saved execution facts, never a moving catalog or loaded-model list.
func Models(presets []Preset) ([]Model, error) {
	models := []Model{}
	for _, saved := range presets {
		if err := saved.Lock.Validate(); err != nil {
			return nil, err
		}
		p := saved.Lock.Records.Presets[saved.Lock.Preset]
		if p.Interface != "chat-completions" {
			continue
		}
		input := []string{"text"}
		if slices.Contains(p.Modalities, "image") {
			input = append(input, "image")
		}
		window := p.Window()
		if c := p.Config(); c.Kind == "llama-server/v1" {
			total, err := modelmemory.LlamaTokens(window, c.Parallel)
			if err != nil {
				return nil, err
			}
			window = int(total / int64(c.Parallel))
		}
		limit := min(p.RequestDefaults.MaxOutputTokens, window)
		var sampling map[string]any
		if limit == 0 {
			limit = window
			sampling = map[string]any{"max_tokens": nil, "max_completion_tokens": nil}
		}
		models = append(models, Model{ID: saved.ID, Name: saved.Name, ContextWindow: window, MaxTokens: limit, Input: input, Reasoning: p.RequestDefaults.Reasoning == "on",
			Cost:           map[string]int{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			Compat:         map[string]any{"supportsStore": false, "supportsDeveloperRole": false, "supportsReasoningEffort": false, "maxTokensField": "max_tokens"},
			SamplingParams: sampling})
	}
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return models, nil
}

func endpoint(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	n, e := strconv.Atoi(port)
	if err != nil || e != nil || host != "127.0.0.1" || n < 1024 || n > 65535 {
		return "", fmt.Errorf("invalid Temper listener %q", listen)
	}
	return "http://" + listen + "/v1", nil
}
