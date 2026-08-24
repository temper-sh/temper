package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalog"
	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogsigning"
	"github.com/temper-sh/temper/internal/software/catalogtrust"
)

type testCapabilities struct{}

func (testCapabilities) ValidateCatalog(catalog.Document) error { return nil }

func TestSignDryRunCommitSecondRunReplaceAndVerify(t *testing.T) {
	tool, seedInput := testTool(t)
	directory := t.TempDir()
	artifactPath := filepath.Join(directory, "channel.yaml")
	outputPath := filepath.Join(directory, "channel.signature.yaml")
	first := []byte("schema: temper-software-channel/v1\nchannel: stable\ncatalog:\n  schema: temper-software-supply/v1\n  sequence: 1\n  sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n  locator: https://example.test/catalog/a/\n")
	if err := os.WriteFile(artifactPath, first, 0o644); err != nil {
		t.Fatal(err)
	}

	dryArguments := []string{"sign", "--kind", "channel", "--channel", "stable", "--artifact", artifactPath, "--output", outputPath, "--dry-run"}
	code, stdout, stderr := invoke(t, tool, seedInput, dryArguments)
	if code != 0 || stdout != "RESULT catalog-sign would-create kind=channel key="+catalogtrust.ProductionKeyID+"\n" || stderr != "" {
		t.Fatalf("dry run = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("dry run output exists or stat failed: %v", err)
	}

	arguments := dryArguments[:len(dryArguments)-1]
	code, stdout, stderr = invoke(t, tool, seedInput, arguments)
	if code != 0 || stdout != "RESULT catalog-sign created kind=channel key="+catalogtrust.ProductionKeyID+"\n" || stderr != "" {
		t.Fatalf("create = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	firstSignature, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = invoke(t, tool, seedInput, arguments)
	if code != 0 || stdout != "RESULT catalog-sign unchanged kind=channel key="+catalogtrust.ProductionKeyID+"\n" || stderr != "" {
		t.Fatalf("second run = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	verifyArguments := []string{"verify", "--kind", "channel", "--channel", "stable", "--artifact", artifactPath, "--signature", outputPath}
	code, stdout, stderr = invoke(t, tool, nil, verifyArguments)
	if code != 0 || stdout != "RESULT catalog-verify valid kind=channel key="+catalogtrust.ProductionKeyID+"\n" || stderr != "" {
		t.Fatalf("verify = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	second := bytes.Replace(first, []byte("sequence: 1"), []byte("sequence: 2"), 1)
	if err := os.WriteFile(artifactPath, second, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = invoke(t, tool, seedInput, arguments)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "rerun with --replace") {
		t.Fatalf("replacement refusal = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	unchanged, err := os.ReadFile(outputPath)
	if err != nil || !bytes.Equal(unchanged, firstSignature) {
		t.Fatalf("refusal changed output = %q, error %v", unchanged, err)
	}

	replaceArguments := append(append([]string(nil), arguments...), "--replace")
	code, stdout, stderr = invoke(t, tool, seedInput, replaceArguments)
	if code != 0 || stdout != "RESULT catalog-sign replaced kind=channel key="+catalogtrust.ProductionKeyID+"\n" || stderr != "" {
		t.Fatalf("replace = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	replaced, err := os.ReadFile(outputPath)
	if err != nil || bytes.Equal(replaced, firstSignature) {
		t.Fatalf("replace output unchanged, error %v", err)
	}
}

func TestSignDoesNotEchoSeedAndRefusesArtifactAsOutput(t *testing.T) {
	tool, _ := testTool(t)
	directory := t.TempDir()
	artifactPath := filepath.Join(directory, "channel.yaml")
	artifact := []byte("schema: temper-software-channel/v1\nchannel: stable\ncatalog:\n  schema: temper-software-supply/v1\n  sequence: 1\n  sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n  locator: https://example.test/catalog/a/\n")
	if err := os.WriteFile(artifactPath, artifact, 0o644); err != nil {
		t.Fatal(err)
	}
	secretInput := "definitely-not-a-seed"
	arguments := []string{"sign", "--kind", "channel", "--channel", "stable", "--artifact", artifactPath, "--output", filepath.Join(directory, "signature.yaml")}
	code, stdout, stderr := invoke(t, tool, []byte(secretInput), arguments)
	if code != 1 || stdout != "" || strings.Contains(stderr, secretInput) {
		t.Fatalf("bad seed = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	arguments[len(arguments)-1] = artifactPath
	code, stdout, stderr = invoke(t, tool, []byte(secretInput), arguments)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "must not be the artifact") {
		t.Fatalf("same output = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func testTool(t *testing.T) (catalogsigning.Tool, []byte) {
	t.Helper()
	seed := bytes.Repeat([]byte{11}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	trust, err := publication.NewTrustRoot(map[string]ed25519.PublicKey{catalogtrust.ProductionKeyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	tool, err := catalogsigning.New(catalogtrust.ProductionKeyID, trust, testCapabilities{})
	if err != nil {
		t.Fatal(err)
	}
	return tool, []byte(base64.StdEncoding.EncodeToString(seed) + "\n")
}

func invoke(t *testing.T, tool catalogsigning.Tool, stdin []byte, arguments []string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runWithTool(context.Background(), arguments, bytes.NewReader(stdin), &stdout, &stderr, tool)
	return code, stdout.String(), stderr.String()
}
