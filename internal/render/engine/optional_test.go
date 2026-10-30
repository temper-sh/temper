package engine_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestPartialSamplingAndEffortOmitOnlyUnsetFlags(t *testing.T) {
	r := validLlamaServerRequest()
	r.LlamaServer.Controls = &engine.LlamaServerControls{Fit: "off", LoadMode: "mmap"}
	r.Sampling = &engine.SamplingDefaults{Temperature: new(float64(0)), TopK: new(0)}
	command, err := engine.Build(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--reasoning-effort", "--top-p", "--min-p", "--repeat-penalty", "--presence-penalty", "--ctx-checkpoints", "--cache-ram"} {
		if strings.Contains(strings.Join(command.Lines(), " "), flag) {
			t.Errorf("omitted setting emitted %s", flag)
		}
	}
	for _, flag := range []string{"--temp 0", "--top-k 0"} {
		if !strings.Contains(strings.Join(command.Lines(), " "), flag) {
			t.Errorf("explicit zero lost: %s", flag)
		}
	}
	r.Sampling.TopP = new(float64(0))
	if _, err := engine.Build(r); err == nil {
		t.Fatal("accepted invalid explicit top_p")
	}
}

func TestSplashV2OmissionAndLegacyTimeoutBoundary(t *testing.T) {
	r := engine.Request{Engine: engine.Splash, LayoutID: "test", ModelPath: "/model", ArtifactFormat: "gguf", Interface: engine.InterfaceChatCompletions, Modalities: []string{"text"}, Window: 8192, Thinking: "on", Speculation: "dflash2",
		Splash: &engine.SplashTuning{Adapter: "splash/v2", ModelID: "test", AssemblyPath: "/assembly", StatePath: "/state", SplashConfig: engine.SplashConfig{KVCache: "int8", MaxMemoryBytes: 24 << 30}}}
	c, err := engine.Build(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(c.Arguments(), " "), "--request-timeout") || strings.Contains(strings.Join(c.Arguments(), " "), "--default-reasoning-effort") || len(c.Runtime().DefaultParameters) != 0 {
		t.Fatal("invented defaults", c)
	}
	r.Thinking = "off"
	c, err = engine.Build(r)
	if err != nil || !strings.Contains(strings.Join(c.Arguments(), " "), "--default-reasoning-effort none") {
		t.Fatal("off unnecessarily requires an explicit effort", err)
	}
	r.Thinking = "on"
	r.Splash.RequestTimeoutSeconds = new(0)
	if _, err := engine.Build(r); err == nil {
		t.Fatal("accepted zero timeout")
	}
	r.Splash.RequestTimeoutSeconds = new(600)
	r.Sampling = &engine.SamplingDefaults{Temperature: new(float64(0))}
	c, err = engine.Build(r)
	if err != nil || !strings.Contains(strings.Join(c.Arguments(), " "), "--request-timeout 600") || c.Runtime().DefaultParameters["temperature"] != float64(0) {
		t.Fatal(c, err)
	}
	r.Sampling.RepeatPenalty = new(1.1)
	if _, err := engine.Build(r); err == nil {
		t.Fatal("accepted unsupported repetition penalty")
	}
	r.Sampling = nil
	r.Splash.Adapter = "splash/v1"
	r.Splash.RequestTimeoutSeconds = nil
	if _, err := engine.Build(r); err == nil {
		t.Fatal("silently changed legacy timeout semantics")
	}
}
