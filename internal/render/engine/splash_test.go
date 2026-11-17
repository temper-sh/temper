package engine_test

import (
	"slices"
	"testing"

	"github.com/temper-sh/temper/internal/render/engine"
)

func TestSplashReleaseSelectsTemplateAndExactNativeArguments(t *testing.T) {
	for _, version := range []string{"", "1.3.2", "1.7.0"} {
		for _, kv := range []string{"int8", "bf16"} {
			t.Run(version+"/"+kv, func(t *testing.T) {
				r := engine.Request{
					Engine: engine.Splash, EngineVersion: version, LayoutID: "qwen", ModelPath: "/target.gguf",
					ArtifactFormat: "gguf", Interface: engine.InterfaceChatCompletions, Modalities: []string{"text"},
					Window: 32768, Thinking: "on", Speculation: "dflash2", ChatTemplatePath: "/selected patch/frog.jinja",
					Splash: &engine.SplashTuning{Adapter: "splash/v2", ModelID: "owner/qwen", AssemblyPath: "/prepared path/models/.resolved/exact", StatePath: "/state",
						SplashConfig: engine.SplashConfig{KVCache: kv, MaxMemoryBytes: 24 << 30}},
				}
				command, err := engine.Build(r)
				if err != nil {
					t.Fatal(err)
				}
				runtime := command.Runtime()
				args := runtime.Requirement.Arguments
				modern := version == "1.7.0"
				if slices.Contains(args, "--tokenizer") == modern {
					t.Fatal("wrong tokenizer interface", args)
				}
				i := slices.Index(args, "--chat-template-file")
				if (i >= 0) != modern || modern && args[i+1] != r.ChatTemplatePath {
					t.Fatal("selected template lost", args)
				}
				if !slices.Contains(args, r.Splash.AssemblyPath) {
					t.Fatal("frontend lost concrete prepared directory", args)
				}
				want := []string{"serve-native", r.Splash.AssemblyPath, "32768", "25769803776", "0"}
				if kv != "int8" {
					want = append(want, "--kv-format", kv)
				}
				if len(runtime.AdditionalRequirements) != 1 || !slices.Equal(runtime.AdditionalRequirements[0].Arguments, want) {
					t.Fatal("native ownership arguments differ from frontend launch", runtime.AdditionalRequirements)
				}
				var relaunch []string
				if modern {
					relaunch = append(slices.Clone(want), "--relaunch-context", "32768")
				}
				if !slices.Equal(runtime.AdditionalRequirements[0].RelaunchArguments, relaunch) {
					t.Fatal("relaunch must name the same prepared model, settings and explicit context", runtime.AdditionalRequirements)
				}
				if modern {
					runtime.AdditionalRequirements[0].RelaunchArguments[1] = "/changed"
					if !slices.Equal(command.Runtime().AdditionalRequirements[0].RelaunchArguments, relaunch) {
						t.Fatal("caller changed the command's owned relaunch identity")
					}
				}
				if slices.ContainsFunc(runtime.Environment, func(e engine.EnvironmentAssignment) bool { return e.Name == "SPLASH_WEIGHT_CACHE" }) == modern {
					t.Fatal("wrong release cache environment")
				}
			})
		}
	}
}

func TestSplashRefusesUnimplementedReleaseInterfaces(t *testing.T) {
	for _, pair := range [][2]string{{"1.4.0", "splash/v2"}, {"1.6.1", "splash/v2"}, {"1.7.0", "splash/v1"}, {"2.0.0", "splash/v2"}, {"invalid", "splash/v2"}} {
		r := engine.Request{Engine: engine.Splash, EngineVersion: pair[0], Splash: &engine.SplashTuning{Adapter: pair[1]}}
		if _, err := engine.Build(r); err == nil {
			t.Fatalf("unsupported contract accepted: %v", pair)
		}
	}
}
