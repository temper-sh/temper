package render_test

import (
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/lockfile"
	"github.com/temper-sh/temper/internal/manifest"
	"github.com/temper-sh/temper/internal/render"
)

func TestCurrentPostureManifestRendersLegacySemanticsOrBetter(t *testing.T) {
	manifestBytes := readPostureFixture(t, "manifest.yaml")
	lockBytes := readPostureFixture(t, "manifest.lock.yaml")
	document, err := manifest.Parse(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := lockfile.Parse(lockBytes)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := render.Build(render.Inputs{
		Manifest:       document,
		Lock:           locked,
		Mode:           "local",
		Root:           "/temper",
		PiModelsBase:   []byte(`{"providers":{"remote":{"baseUrl":"https://example.invalid"}},"ownerKey":"preserved"}`),
		PiSettingsBase: []byte(`{"defaultModel":null,"theme":"dark","compaction":{"enabled":true,"ownerKey":"preserved"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		artifact   string
		contains   []string
		notContain []string
	}{
		{
			name:       "llama-swap global posture and no implicit preload",
			artifact:   "llama-swap/config.yaml",
			contains:   []string{"healthCheckTimeout: 300", "startPort: 10001", "globalTTL: 1800", "captureBuffer: 15"},
			notContain: []string{"hooks:", "preload:"},
		},
		{
			name:     "coder tuning and tested locked artifacts",
			artifact: "llama-swap/config.yaml",
			contains: []string{
				`"qwen3.8-27b-gguf-24k"`,
				`name: "Qwen3.8 27B GGUF 24k (stable window)"`,
				"-m '/temper/artifacts/layouts/qwen3.8-27b-gguf-24k/40a0c4c47fb70d6cda53cdfb485b6f1217d8d315f15ece44eab639b49fb9cd4b/model/Qwen3.8-27B-Q4_K_M.gguf'",
				"--offline\n      --no-mmproj\n      --jinja\n      --parallel 1\n      -c 24576\n      -fa on",
				"-ctk q8_0 -ctv q8_0\n      -b 512\n      -ub 512",
				"--chat-template-file '/temper/artifacts/layouts/qwen3.8-27b-gguf-24k/40a0c4c47fb70d6cda53cdfb485b6f1217d8d315f15ece44eab639b49fb9cd4b/patches/qwen38-sharp-template/chat_template.jinja'",
				`--chat-template-kwargs '{"enable_thinking":false}'`,
				"ttl: 7200",
			},
			notContain: []string{"-hf unsloth/Qwen3.8-27B-GGUF"},
		},
		{
			name:     "CPU reranker tuning and on-demand placement",
			artifact: "llama-swap/config.yaml",
			contains: []string{
				`"rerank-qwen3-0.6b"`,
				"-m '/temper/artifacts/layouts/rerank-qwen3-0.6b/3c6edd6c4a6953aaad7ffa2b1f4843358679af84b47900acf0373d002cfb99c8/model/Qwen3-Reranker-0.6B.Q8_0.gguf'",
				"--offline\n      --no-mmproj\n      --reranking\n      --parallel 1\n      -c 4096\n      -fa auto\n      -b 256\n      -ub 256\n      -ngl 0",
				"ttl: 120",
				`members: ["rerank-qwen3-0.6b"]`,
			},
		},
		{
			name:     "Pi receives only the coder and preserves unowned providers",
			artifact: "pi/models.json",
			contains: []string{
				`"id": "qwen3.8-27b-gguf-24k"`,
				`"contextWindow": 24576`,
				`"maxTokens": 4096`,
				`"reasoning": false`,
				`"remote"`,
				`"ownerKey": "preserved"`,
			},
			notContain: []string{`"id": "rerank-qwen3-0.6b"`},
		},
		{
			name:     "Pi preference and compaction are explicit and derived",
			artifact: "pi/settings.json",
			contains: []string{
				`"defaultModel": "qwen3.8-27b-gguf-24k"`,
				`"reserveTokens": 3072`,
				`"keepRecentTokens": 7168`,
				`"theme": "dark"`,
				`"ownerKey": "preserved"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := string(artifact(t, bundle, test.artifact))
			for _, wanted := range test.contains {
				if !strings.Contains(actual, wanted) {
					t.Errorf("%s does not contain %q\n%s", test.artifact, wanted, actual)
				}
			}
			for _, unwanted := range test.notContain {
				if strings.Contains(actual, unwanted) {
					t.Errorf("%s unexpectedly contains %q\n%s", test.artifact, unwanted, actual)
				}
			}
		})
	}
}

func readPostureFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/current-posture/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
