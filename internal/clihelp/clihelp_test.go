package clihelp

import (
	"bytes"
	"errors"
	"flag"
	"net/url"
	"strings"
	"testing"
)

func TestPrintShowsPurposeExamplesAndReadableOptions(t *testing.T) {
	f := flag.NewFlagSet("temper update", flag.ContinueOnError)
	f.String("root", "", "Temper folder (default ~/.temper)")
	f.Bool("dry-run", false, "show what would change; change nothing")
	f.String("listen", "127.0.0.1:8080", "address to serve on")
	var out bytes.Buffer
	Print(&out, Help{Summary: "Update your presets.", Usage: []string{"temper update [options]"}, Examples: []string{"temper update --dry-run"}}, f)
	for _, want := range []string{"Update your presets.", "Usage:\n  temper update [options]", "Examples:\n  temper update --dry-run",
		"--dry-run ", "--root VALUE", "(default 127.0.0.1:8080)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Usage of") || strings.Contains(out.String(), "\t") {
		t.Errorf("raw flag dump leaked:\n%s", out.String())
	}
}

func TestClosestSuggestsLikelyPresetIDs(t *testing.T) {
	ids := []string{"qwen3.8-27b-q4xl-mtp", "qwen3.5-4b-q4km-off", "gemma-4-26b-a4b-qat-ud-q4-k-xl-llama", "nuextract3-q4km-llama"}
	for id, want := range map[string]string{
		"qwen3.8-27b-q4xl":     "qwen3.8-27b-q4xl-mtp",
		"qwen3.5-4b-q4km":      "qwen3.5-4b-q4km-off",
		"gemma-4-26b":          "gemma-4-26b-a4b-qat-ud-q4-k-xl-llama",
		"nuextract3-q4km-lama": "nuextract3-q4km-llama",
		"completely-different": "",
	} {
		if got := Closest(id, ids); got != want {
			t.Errorf("Closest(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestUnknownIDSuggestsAndSaysWhereToList(t *testing.T) {
	err := UnknownID("preset", "qwen-27b-q4xl", []string{"qwen3.8-27b-q4xl-mtp", "gemma-4-e2b"}, "temper catalog inspect --root R")
	want := `preset "qwen-27b-q4xl" is not in the catalog; did you mean qwen3.8-27b-q4xl-mtp? List them with: temper catalog inspect --root R`
	if err.Error() != want {
		t.Fatalf("got %q\nwant %q", err, want)
	}
}

func TestNetworkErrorsSayWhatToCheck(t *testing.T) {
	cause := &url.Error{Op: "Get", URL: "https://temper-sh.github.io/catalog/v1/channels/stable/channel.yaml", Err: errors.New("proxyconnect tcp: dial tcp 127.0.0.1:9: connect: connection refused")}
	err := Network("download the catalog", cause)
	if !errors.Is(err, cause) || !strings.HasPrefix(err.Error(), "could not download the catalog: ") || !strings.Contains(err.Error(), "proxy settings") {
		t.Fatalf("network error = %v", err)
	}
	other := errors.New("signature does not verify")
	if Network("download the catalog", other) != other || Network("x", nil) != nil {
		t.Fatal("non-network errors must pass through unchanged")
	}
}
