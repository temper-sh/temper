package engine

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	semver "github.com/Masterminds/semver/v3"
)

const SplashPython = "python/bin/python3.13"
const SplashNative = "engine/splash"

// SplashBootstrap invokes the release's frontend with its bundled interpreter.
// It performs no model resolution, download or package installation.
const SplashBootstrap = "import os,pathlib,runpy,sys; os.environ.pop('SPLASH_API_KEY',None); p=pathlib.Path(sys.executable).resolve().parents[2]; s=p/'server'/'server.py'; sys.path.insert(0,str(s.parent)); sys.argv=[str(s),*sys.argv[1:],'--binary',str(p/'engine'/'splash')]; runpy.run_path(str(s),run_name='__main__')"

// SplashSourceBootstrap serves the source-assembly interface introduced in 1.3.
const SplashSourceBootstrap = "import os,pathlib,runpy,sys; os.environ.pop('SPLASH_API_KEY',None); p=pathlib.Path(sys.executable).resolve().parents[2]; sys.path.insert(0,str(p)); sys.argv=['server.server',*sys.argv[1:],'--binary',str(p/'engine'/'splash')]; runpy.run_module('server.server',run_name='__main__')"

type SplashConfig struct {
	KVCache               string `yaml:"kv_cache" json:"kv_cache"`
	MaxMemoryBytes        int64  `yaml:"max_memory_bytes" json:"max_memory_bytes"`
	ReasoningEffort       string `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"`
	RequestTimeoutSeconds *int   `yaml:"request_timeout_seconds,omitempty" json:"request_timeout_seconds,omitempty"`
}

type SplashTuning struct {
	SplashConfig
	Adapter      string
	ModelID      string
	AssemblyPath string
	StatePath    string
}

func ValidateSplashAdapter(adapter string) error {
	if adapter != "" && adapter != "splash/v1" && adapter != "splash/v2" {
		return errors.New("unsupported Splash adapter")
	}
	return nil
}

// SplashLocalPreparation identifies the release-owned local preparation API.
// Empty versions preserve the launch contract of existing manifests.
func SplashLocalPreparation(version string) (bool, error) {
	if version == "" {
		return false, nil
	}
	v, err := semver.NewVersion(version)
	if err != nil {
		return false, fmt.Errorf("Splash engine version: %w", err)
	}
	if v.Major() != 1 {
		return false, errors.New("unsupported Splash major version; this adapter supports Splash 1.x")
	}
	return !v.LessThan(semver.MustParse("1.7.0")), nil
}

func (c SplashConfig) Validate() error {
	if c.KVCache != "int8" && c.KVCache != "bf16" {
		return errors.New("Splash kv_cache must be int8 or bf16")
	}
	if c.MaxMemoryBytes <= 0 || c.RequestTimeoutSeconds != nil && *c.RequestTimeoutSeconds <= 0 {
		return errors.New("Splash requires positive memory and request timeout limits")
	}
	switch c.ReasoningEffort {
	case "", "none", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return errors.New("Splash reasoning_effort must be none, minimal, low, medium, high, xhigh or max")
	}
	return nil
}

func buildSplash(r Request) (Command, error) {
	c := r.Splash
	local, err := SplashLocalPreparation(r.EngineVersion)
	if err != nil {
		return Command{}, err
	}
	if local && c.Adapter != "splash/v2" {
		return Command{}, errors.New("Splash 1.7 or newer requires the splash/v2 adapter")
	}
	if !local && r.EngineVersion != "" {
		v, _ := semver.NewVersion(r.EngineVersion)
		if !v.LessThan(semver.MustParse("1.4.0")) {
			return Command{}, errors.New("Splash 1.4–1.6 removed the tokenizer override; use 1.3.2 or 1.7 or newer")
		}
	}
	if err := ValidateSplashAdapter(c.Adapter); err != nil {
		return Command{}, err
	}
	if err := c.SplashConfig.Validate(); err != nil {
		return Command{}, err
	}
	if c.Adapter != "splash/v2" {
		if c.RequestTimeoutSeconds == nil {
			return Command{}, errors.New("Splash v1 requires an explicit request timeout")
		}
		switch c.ReasoningEffort {
		case "", "none", "low", "medium", "xhigh":
		default:
			return Command{}, errors.New("Splash v1 reasoning_effort must be none, low, medium or xhigh")
		}
	}
	common := r
	common.ChatTemplatePath = "" // Legacy: prepared tokenizer. 1.7+: explicit option below.
	if err := validateChatRequest(common, Splash, "gguf", []string{"text"}); err != nil {
		return Command{}, err
	}
	if r.Speculation != "dflash2" || r.SpeculativeTokens != 0 {
		return Command{}, errors.New("Splash requires its DFlash2 sidecar; draft block size is engine-owned")
	}
	if c.ReasoningEffort != "" && (r.Thinking == "off") != (c.ReasoningEffort == "none") {
		return Command{}, errors.New("Splash reasoning off and reasoning_effort none go together")
	}
	if r.Window > 262144 {
		return Command{}, errors.New("Splash context must be at most 262144 tokens")
	}
	if c.ModelID == "" || !filepath.IsAbs(c.AssemblyPath) || !filepath.IsAbs(c.StatePath) {
		return Command{}, errors.New("Splash requires a model ID and absolute prepared-material/state paths")
	}
	s := &SamplingDefaults{}
	if r.Sampling != nil {
		s = r.Sampling
	}
	if err := s.Validate(); err != nil {
		return Command{}, err
	}
	if s.Temperature != nil && *s.Temperature > 2 || s.TopK != nil && (*s.TopK < 1 || *s.TopK > 32) || nonNeutral(s.MinP, 0) || nonNeutral(s.RepeatPenalty, 1) || nonNeutral(s.PresencePenalty, 0) || s.Seed != nil && *s.Seed < 0 {
		return Command{}, errors.New("Splash supports temperature up to 2, top_k 1–32, a nonnegative seed, and neutral penalties")
	}
	target, draft := filepath.Join(c.AssemblyPath, "target"), filepath.Join(c.AssemblyPath, "draft")
	maxTokens := r.MaxTokens
	if maxTokens == 0 {
		// Splash clamps its server default to context minus the rendered prompt.
		maxTokens = r.Window
	}
	args := []string{"-I", "-B", "-u", "-c", SplashBootstrap, target, draft}
	if c.Adapter == "splash/v2" {
		// Keep the new SSD prefix cache disabled until a catalog policy selects
		// it deliberately. ANE prefill and fp32 recurrent state are 1.3 defaults.
		args = []string{"-I", "-B", "-u", "-c", SplashSourceBootstrap, c.AssemblyPath, "--max-cache-disk", "0", "--state-format", "fp32"}
	}
	if local {
		if r.ChatTemplatePath != "" {
			if err := validateCommandData(r.ChatTemplatePath); err != nil {
				return Command{}, err
			}
			args = append(args, "--chat-template-file", r.ChatTemplatePath)
		}
	} else {
		args = append(args, "--tokenizer", filepath.Join(c.AssemblyPath, "tokenizer"))
	}
	args = append(args, "--model", c.ModelID,
		"--served-model-name", r.LayoutID, "--host", "127.0.0.1", "--port", "${PORT}",
		"--max-memory", strconv.FormatInt(c.MaxMemoryBytes, 10), "--max-context", strconv.Itoa(r.Window),
		"--kv-format", c.KVCache)
	effort := c.ReasoningEffort
	if r.Thinking == "off" {
		effort = "none"
	}
	if effort != "" {
		args = append(args, "--default-reasoning-effort", effort)
	}
	defaults := splashDefaults(*s)
	if c.Adapter == "splash/v2" {
		// 1.3 uses remaining context natively; an explicit preset cap is a
		// request default, preserved only when the client supplies no override.
		if r.MaxTokens > 0 {
			defaults["max_tokens"] = r.MaxTokens
		}
	} else {
		args = append(args, "--max-new-tokens", strconv.Itoa(maxTokens))
	}
	if c.RequestTimeoutSeconds != nil {
		args = append(args, "--request-timeout", strconv.Itoa(*c.RequestTimeoutSeconds))
	}
	args = append(args, "--no-webui")
	var groups [][]commandWord
	for _, arg := range args {
		word := dataWord(arg)
		if arg == "${PORT}" {
			word = portWord()
		}
		groups = append(groups, []commandWord{word})
	}
	nativeArgs := []string{"serve-native", target, draft, strconv.Itoa(r.Window), strconv.FormatInt(c.MaxMemoryBytes, 10)}
	if c.Adapter == "splash/v2" {
		nativeArgs = []string{"serve-native", c.AssemblyPath, strconv.Itoa(r.Window), strconv.FormatInt(c.MaxMemoryBytes, 10), "0"}
	}
	if c.KVCache != "int8" {
		nativeArgs = append(nativeArgs, "--kv-format", c.KVCache)
	}
	var relaunchArgs []string
	if local {
		// With explicit --max-context, the first engine either serves that
		// context or fails startup. Every relaunch asks for that first context.
		relaunchArgs = append(append([]string(nil), nativeArgs...), "--relaunch-context", strconv.Itoa(r.Window))
	}
	environment := []EnvironmentAssignment{{Name: "HOME", Value: filepath.Join(c.StatePath, "home")}}
	if !local {
		// Retained for saved releases. Current Splash no longer uses a weight cache.
		environment = append(environment, EnvironmentAssignment{Name: "SPLASH_WEIGHT_CACHE", Value: filepath.Join(c.StatePath, "weights")})
	}
	environment = append(environment, EnvironmentAssignment{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"}, EnvironmentAssignment{Name: "SPLASH_CRASH_TRACE", Value: "0"})
	return commandFromLaunch(launchSpec{executable: knownWord("python3.13"), argumentGroups: groups}, Runtime{
		Requirement:            RuntimeRequirement{Package: Splash, RelativeExecutable: SplashPython, Role: "frontend", Arguments: args},
		AdditionalRequirements: []RuntimeRequirement{{Package: Splash, RelativeExecutable: SplashNative, Role: "engine", Arguments: nativeArgs, RelaunchArguments: relaunchArgs}},
		Environment:            offlineEnvironment(environment...),
		CheckEndpoint:          "/health", UseModelName: r.LayoutID, ContextWindow: r.Window,
		DefaultParameters: defaults,
	})
}

// splashDefaults are Splash's request defaults; a seed only when one is set.
func splashDefaults(s SamplingDefaults) map[string]any {
	defaults := map[string]any{}
	if s.Temperature != nil {
		defaults["temperature"] = *s.Temperature
	}
	if s.TopK != nil {
		defaults["top_k"] = *s.TopK
	}
	if s.TopP != nil {
		defaults["top_p"] = *s.TopP
	}
	if s.Seed != nil {
		defaults["seed"] = *s.Seed
	}
	return defaults
}

func nonNeutral(value *float64, neutral float64) bool { return value != nil && *value != neutral }
