package patch_test

import (
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/patch"
)

const patchRevision = "1111111111111111111111111111111111111111"

func TestParseSource(t *testing.T) {
	t.Parallel()
	got, err := patch.ParseSource("hf://owner/repo@" + patchRevision + "/templates/chat.jinja?transform=qwen38-prefix-stability-v1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Repo != "owner/repo" || got.Revision != patchRevision || got.File != "templates/chat.jinja" || got.Transform != patch.Qwen38PrefixStabilityV1 {
		t.Fatalf("source = %#v", got)
	}
}

func TestParseSourceRefusesUnpinnedAndUnknownQuery(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"hf://owner/repo@main/template.jinja",
		"hf://owner/repo@" + patchRevision + "/template.jinja?other=value",
		"hf://owner/repo@" + patchRevision + "/template.jinja?transform=unknown-v1",
		"https://owner/repo@" + patchRevision + "/template.jinja",
	} {
		if _, err := patch.ParseSource(source); err == nil {
			t.Errorf("ParseSource(%q) succeeded", source)
		}
	}
}

func TestQwen38TransformChangesExactlyOneGuard(t *testing.T) {
	t.Parallel()
	old := "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and reasoning_content %}"
	source := []byte("before\n" + old + "\nafter\n")
	got, err := patch.Apply(patch.Qwen38PrefixStabilityV1, source)
	if err != nil {
		t.Fatal(err)
	}
	want := "and (reasoning_content or not ns_state.thinking) %}"
	if !strings.Contains(string(got), want) || strings.Contains(string(got), "and reasoning_content %}") {
		t.Fatalf("transformed source = %q", got)
	}
}

func TestQwen38TransformRefusesSourceDrift(t *testing.T) {
	t.Parallel()
	old := "{%- if (_preserve_thinking or loop.index0 > ns.last_query_index) and reasoning_content %}"
	for _, source := range []string{"no match", old + "\n" + old} {
		if _, err := patch.Apply(patch.Qwen38PrefixStabilityV1, []byte(source)); err == nil {
			t.Errorf("Apply to %q succeeded", source)
		}
	}
}
