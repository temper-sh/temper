package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

func publishRun(t *testing.T, tool catalogsigning.Tool, seed []byte, args ...string) (int, string, string) {
	t.Helper()
	return invoke(t, tool, seed, append([]string{"publish"}, args...))
}

// source writes the frozen catalog, optionally edited, as an authored source.
func source(t *testing.T, edit func(map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile("../../internal/testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		var tree map[string]any
		if err := json.Unmarshal(raw, &tree); err != nil {
			t.Fatal(err)
		}
		edit(tree)
		if raw, err = json.MarshalIndent(tree, "", "  "); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "apple-silicon.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPublishCreatesKeepsAndAdvancesTheChannel(t *testing.T) {
	tool, seed, trust := testTool(t)
	root := filepath.Join(t.TempDir(), "catalog", "v1")
	first := source(t, nil)
	code, stdout, stderr := publishRun(t, tool, seed, "--source", first, "--root", root, "--dry-run")
	if code != 0 || !strings.HasPrefix(stdout, "Dry run: would publish apple-silicon catalog sequence 1 ") {
		t.Fatalf("dry run: %d %q %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("dry run wrote the tree: %v", err)
	}
	if code, stdout, stderr = publishRun(t, tool, seed, "--source", first, "--root", root); code != 0 || !strings.HasPrefix(stdout, "Published apple-silicon catalog sequence 1 ") {
		t.Fatalf("publish: %d %q %q", code, stdout, stderr)
	}
	verify := func(src string) (int, string) {
		var out, diagnostics bytes.Buffer
		code := runVerifyPublication([]string{"--root", root, "--source", src}, &out, &diagnostics, trust)
		return code, out.String() + diagnostics.String()
	}
	if code, output := verify(first); code != 0 || !strings.Contains(output, "is verified: sequence 1,") {
		t.Fatalf("verify-publication: %d %s", code, output)
	}
	if code, stdout, _ = publishRun(t, tool, seed, "--source", first, "--root", root); code != 0 || !strings.HasPrefix(stdout, "Already published: apple-silicon catalog sequence 1 ") {
		t.Fatalf("rerun: %d %q", code, stdout)
	}

	// Another platform's entry, published by a later tool.
	channelDir := filepath.Join(root, "channels", "stable")
	raw, _ := os.ReadFile(filepath.Join(channelDir, "channel.yaml"))
	signature, _ := os.ReadFile(filepath.Join(channelDir, "channel.signature.yaml"))
	verified, err := publication.VerifyChannel("stable", raw, signature, trust)
	if err != nil {
		t.Fatal(err)
	}
	otherCatalog := []byte(`{"platform": "linux-cuda"}`)
	otherSHA := fmt.Sprintf("%x", sha256.Sum256(otherCatalog))
	parsedSeed, _ := catalogsigning.ParseSeed(seed)
	otherSignature := testSignature(parsedSeed, otherCatalog)
	if err := os.MkdirAll(filepath.Join(root, "snapshots", otherSHA), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"catalog.json": otherCatalog, "catalog.signature.yaml": otherSignature} {
		if err := os.WriteFile(filepath.Join(root, "snapshots", otherSHA, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	withOther, err := verified.Document.WithCatalog("linux-cuda", publication.CatalogReference{Schema: "temper-catalog/v1", SHA256: otherSHA, Locator: "https://example.test/snapshots/" + otherSHA + "/"})
	if err != nil {
		t.Fatal(err)
	}
	withOther.Sequence++
	channelData, err := publication.MarshalChannel(withOther)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"channel.yaml": channelData, "channel.signature.yaml": testSignature(parsedSeed, channelData)} {
		if err := os.WriteFile(filepath.Join(channelDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	second := source(t, func(tree map[string]any) {
		p := tree["presets"].(map[string]any)["qwen3.5-4b-q4km-off"].(map[string]any)["presentation"].(map[string]any)
		p["description"] = "Reworded for the second publication."
	})
	if code, stdout, stderr = publishRun(t, tool, seed, "--source", second, "--root", root); code != 0 || !strings.HasPrefix(stdout, "Published apple-silicon catalog sequence 3 ") {
		t.Fatalf("second publication: %d %q %q", code, stdout, stderr)
	}
	raw, _ = os.ReadFile(filepath.Join(channelDir, "channel.yaml"))
	if !strings.Contains(string(raw), "linux-cuda:") || !strings.Contains(string(raw), otherSHA) {
		t.Fatalf("another platform's entry was dropped:\n%s", raw)
	}
	if code, output := verify(second); code != 0 {
		t.Fatalf("second verify: %s", output)
	}
	if code, output := verify(first); code == 0 {
		t.Fatalf("a stale source passed: %s", output)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Fatalf("publication left a stage behind: %v", entries)
	}
	var stdoutBuffer, stderrBuffer bytes.Buffer
	if code := run(context.Background(), []string{"verify-publication", "--root", root}, nil, &stdoutBuffer, &stderrBuffer); code == 0 {
		t.Fatal("production command trusted the test signing key")
	}
}

func TestPublishRefusesDatesSeedsAndInvalidSources(t *testing.T) {
	tool, seed, _ := testTool(t)
	root := filepath.Join(t.TempDir(), "v1")
	dated := func(date string) string {
		return source(t, func(tree map[string]any) { tree["date"] = date })
	}
	if code, _, stderr := publishRun(t, tool, seed, "--source", dated("2999-01-01"), "--root", root); code != 1 || !strings.Contains(stderr, "today") {
		t.Fatalf("future date: %d %s", code, stderr)
	}
	if code, _, stderr := publishRun(t, tool, seed, "--source", dated("2026-09-20"), "--root", root); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, stderr := publishRun(t, tool, seed, "--source", dated("2026-09-19"), "--root", root); code != 1 || !strings.Contains(stderr, "2026-09-20") {
		t.Fatalf("earlier date: %d %s", code, stderr)
	}
	secret := "definitely-not-a-seed"
	if code, stdout, stderr := publishRun(t, tool, []byte(secret), "--source", dated("2026-09-21"), "--root", root); code != 1 || stdout != "" || strings.Contains(stderr, secret) {
		t.Fatalf("bad seed: %d %q %q", code, stdout, stderr)
	}
	other := []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{12}, ed25519.SeedSize)))
	if code, _, stderr := publishRun(t, tool, other, "--source", dated("2026-09-21"), "--root", root); code != 1 || !strings.Contains(stderr, "does not match") {
		t.Fatalf("wrong key: %d %s", code, stderr)
	}
	invalid := source(t, func(tree map[string]any) { tree["future"] = true })
	if code, _, stderr := publishRun(t, tool, seed, "--source", invalid, "--root", root); code != 1 || !strings.Contains(stderr, "catalog source") {
		t.Fatalf("unknown key accepted by the publisher: %d %s", code, stderr)
	}
	if code, _, _ := publishRun(t, tool, seed, "--root", root); code != 2 {
		t.Fatal("missing --source accepted")
	}
}

func testSignature(seed, data []byte) []byte {
	key := ed25519.NewKeyFromSeed(seed)
	return []byte(fmt.Sprintf("schema: temper-signature/v1\nkey_id: %s\nalgorithm: ed25519\nsignature: %s\n", catalogtrust.ProductionKeyID, base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))))
}

func testTool(t *testing.T) (catalogsigning.Tool, []byte, publication.TrustRoot) {
	t.Helper()
	seed := bytes.Repeat([]byte{11}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{catalogtrust.ProductionKeyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := catalogsigning.New(catalogtrust.ProductionKeyID, trust)
	if err != nil {
		t.Fatal(err)
	}
	return tool, []byte(base64.StdEncoding.EncodeToString(seed) + "\n"), trust
}

func invoke(t *testing.T, tool catalogsigning.Tool, stdin []byte, arguments []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithTool(context.Background(), arguments, bytes.NewReader(stdin), &stdout, &stderr, tool)
	return code, stdout.String(), stderr.String()
}

func TestAvailabilityReportsPresetsAVersionCanUse(t *testing.T) {
	raw, err := os.ReadFile("../../internal/testfixture/testdata/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	newer := bytes.Replace(raw, []byte(`"adapter": "splash/v1",`), []byte(`"adapter": "splash/v1", "minimum_temper": "0.2.0",`), 1)
	path := filepath.Join(t.TempDir(), "catalog.json")
	if err := os.WriteFile(path, newer, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"availability", "--catalog", path, "--temper-version", "0.1.0"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	out := stdout.String()
	if !strings.Contains(out, "available: qwen3.8-27b-q4xl-mtp") || !strings.Contains(out, `unavailable preset qwen3.8-27b-q4xl-splash: its engine "splash" is unavailable: needs Temper 0.2.0 or newer (this is 0.1.0)`) {
		t.Fatalf("availability:\n%s", out)
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"availability", "--catalog", path, "--json"}, nil, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), `"unavailable": []`) {
		t.Fatalf("a development build should use every preset: %d %s %s", code, &stdout, &stderr)
	}
}
