package engine

import (
	"errors"
	"path/filepath"
	"strconv"
)

const SplashPython = "python/bin/python3.13"
const SplashNative = "engine/splash"

// SplashBootstrap invokes the release's frontend with its bundled interpreter.
// It performs no model resolution, download or package installation.
const SplashBootstrap = "import os,pathlib,runpy,sys; os.environ.pop('SPLASH_API_KEY',None); p=pathlib.Path(sys.executable).resolve().parents[2]; s=p/'server'/'server.py'; sys.path.insert(0,str(s.parent)); sys.argv=[str(s),*sys.argv[1:],'--binary',str(p/'engine'/'splash')]; runpy.run_path(str(s),run_name='__main__')"

type SplashConfig struct {
	KVCache               string `yaml:"kv_cache" json:"kv_cache"`
	MaxMemoryBytes        int64  `yaml:"max_memory_bytes" json:"max_memory_bytes"`
	ReasoningEffort       string `yaml:"reasoning_effort" json:"reasoning_effort"`
	RequestTimeoutSeconds int    `yaml:"request_timeout_seconds" json:"request_timeout_seconds"`
}

type SplashTuning struct {
	SplashConfig
	ModelID      string
	AssemblyPath string
	StatePath    string
}

func (c SplashConfig) Validate() error {
	if c.KVCache != "int8" && c.KVCache != "bf16" {
		return errors.New("Splash kv_cache must be int8 or bf16")
	}
	if c.MaxMemoryBytes <= 0 || c.RequestTimeoutSeconds <= 0 {
		return errors.New("Splash requires positive memory and request timeout limits")
	}
	switch c.ReasoningEffort {
	case "none", "low", "medium", "xhigh":
	default:
		return errors.New("Splash reasoning_effort must be none, low, medium or xhigh")
	}
	return nil
}

func buildSplash(r Request) (Command, error) {
	c := r.Splash
	if err := c.SplashConfig.Validate(); err != nil {
		return Command{}, err
	}
	common := r
	common.ChatTemplatePath = "" // Applied to the derived tokenizer before serving.
	if err := validateChatRequest(common, Splash, "gguf", []string{"text"}); err != nil {
		return Command{}, err
	}
	if r.Speculation != "dflash2" || r.SpeculativeTokens != 0 {
		return Command{}, errors.New("Splash requires its DFlash2 sidecar; draft block size is engine-owned")
	}
	if (r.Thinking == "off") != (c.ReasoningEffort == "none") || r.Window > 262144 {
		return Command{}, errors.New("Splash thinking and context limits disagree with the selected configuration")
	}
	if c.ModelID == "" || !filepath.IsAbs(c.AssemblyPath) || !filepath.IsAbs(c.StatePath) {
		return Command{}, errors.New("Splash requires a model ID and absolute prepared-material/state paths")
	}
	if r.Sampling == nil {
		return Command{}, errors.New("Splash requires explicit request defaults")
	}
	s := r.Sampling
	if err := s.Validate(); err != nil {
		return Command{}, err
	}
	if s.Temperature > 2 || s.TopK < 1 || s.TopK > 32 || s.MinP != 0 || s.RepeatPenalty != 1 || s.PresencePenalty != 0 || s.Seed < 0 {
		return Command{}, errors.New("Splash supports temperature up to 2, top_k 1–32, a nonnegative seed, and neutral penalties")
	}
	target, draft := filepath.Join(c.AssemblyPath, "target"), filepath.Join(c.AssemblyPath, "draft")
	args := []string{"-I", "-B", "-u", "-c", SplashBootstrap, target, draft,
		"--tokenizer", filepath.Join(c.AssemblyPath, "tokenizer"), "--model", c.ModelID,
		"--served-model-name", r.LayoutID, "--host", "127.0.0.1", "--port", "${PORT}",
		"--max-memory", strconv.FormatInt(c.MaxMemoryBytes, 10), "--max-context", strconv.Itoa(r.Window),
		"--kv-format", c.KVCache, "--default-reasoning-effort", c.ReasoningEffort,
		"--max-new-tokens", strconv.Itoa(r.MaxTokens), "--request-timeout", strconv.Itoa(c.RequestTimeoutSeconds), "--no-webui"}
	var groups [][]commandWord
	for _, arg := range args {
		word := dataWord(arg)
		if arg == "${PORT}" {
			word = portWord()
		}
		groups = append(groups, []commandWord{word})
	}
	nativeArgs := []string{"serve-native", target, draft, strconv.Itoa(r.Window), strconv.FormatInt(c.MaxMemoryBytes, 10)}
	if c.KVCache != "int8" {
		nativeArgs = append(nativeArgs, "--kv-format", c.KVCache)
	}
	return commandFromLaunch(launchSpec{executable: knownWord("python3.13"), argumentGroups: groups}, Runtime{
		Requirement:            RuntimeRequirement{Package: Splash, RelativeExecutable: SplashPython, Role: "frontend", Arguments: args},
		AdditionalRequirements: []RuntimeRequirement{{Package: Splash, RelativeExecutable: SplashNative, Role: "engine", Arguments: nativeArgs}},
		Environment: offlineEnvironment(
			EnvironmentAssignment{Name: "HOME", Value: filepath.Join(c.StatePath, "home")},
			EnvironmentAssignment{Name: "SPLASH_WEIGHT_CACHE", Value: filepath.Join(c.StatePath, "weights")},
			EnvironmentAssignment{Name: "PYTHONDONTWRITEBYTECODE", Value: "1"},
			EnvironmentAssignment{Name: "SPLASH_CRASH_TRACE", Value: "0"},
		),
		CheckEndpoint: "/health", UseModelName: r.LayoutID, ContextWindow: r.Window,
		DefaultParameters: map[string]any{"temperature": s.Temperature, "top_k": s.TopK, "top_p": s.TopP, "seed": s.Seed},
	})
}
