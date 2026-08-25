package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

func TestProductPromotionRoundTripsAdoptedLabsFixture(t *testing.T) {
	data := readProductPromotionFixture(t)
	packet, err := qualification.ParseProductPromotionPacket(data)
	if err != nil {
		t.Fatal(err)
	}
	if packet.Schema != qualification.ProductPromotionSchemaV1 || packet.ID != "fake-coder-artifact-lab" || packet.Revision != 1 {
		t.Fatalf("packet identity = %s/%s@%d", packet.Schema, packet.ID, packet.Revision)
	}
	if packet.Target.Schema != qualification.ModelArtifactSchemaV1 || packet.Target.ID != "fake-coder-artifact" {
		t.Fatalf("packet target = %#v", packet.Target)
	}
	if _, ok := packet.Candidate.Spec.(qualification.PromotionModelArtifactSpec); !ok {
		t.Fatalf("candidate spec type = %T", packet.Candidate.Spec)
	}

	encoded, err := qualification.MarshalProductPromotionPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestCompileProductPromotionMatchesLabsPublicProjection(t *testing.T) {
	packet := readProductPromotionFixture(t)
	want := readProductPromotionProfileFixture(t)

	got, err := qualification.CompileProductPromotion(packet, qualification.ProductPromotionInputs{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("compiled projection differs from Labs contract\n got:\n%s\nwant:\n%s", got, want)
	}
	profile, err := qualification.ParseModelArtifactProfile(got)
	if err != nil {
		t.Fatalf("compiled profile is not canonical C7: %v", err)
	}
	if profile.Promotion.SHA256 != qualification.Digest(packet) || profile.Evidence[0].Source.SHA256 != profile.Promotion.SHA256 {
		t.Fatalf("compiled provenance = promotion %#v, evidence %#v", profile.Promotion, profile.Evidence[0].Source)
	}
	for _, forbidden := range []string{
		"fixtures/private/fake-artifact-review.json",
		"fake-private-artifact-review",
		"catalog_consideration",
		"forbidden_generalizations",
		"sanitization",
	} {
		if bytes.Contains(got, []byte(forbidden)) {
			t.Fatalf("compiled public profile contains C8-only/private value %q", forbidden)
		}
	}
}

func TestParseProductPromotionRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readProductPromotionFixture(t))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "unknown field", input: strings.Replace(canonical, "id: fake-coder-artifact-lab", "automatic_publication: true\nid: fake-coder-artifact-lab", 1), want: "field automatic_publication not found"},
		{name: "duplicate key", input: strings.Replace(canonical, "revision: 1\nsanitization:", "revision: 1\nrevision: 2\nsanitization:", 1), want: "mapping key \"revision\" already defined"},
		{name: "multiple documents", input: canonical + "---\nnull\n", want: "multiple YAML documents"},
		{name: "missing final newline", input: strings.TrimSuffix(canonical, "\n"), want: "not canonical"},
		{name: "noncanonical mapping order", input: "schema: temper-labs-product-promotion/v1\n" + strings.Replace(canonical, "schema: temper-labs-product-promotion/v1\n", "", 1), want: "not canonical"},
		{name: "false sanitization", input: strings.Replace(canonical, "public_candidate_reviewed: true", "public_candidate_reviewed: false", 1), want: "must be true"},
		{name: "target body mismatch", input: strings.Replace(canonical, "target:\n  id: fake-coder-artifact\n  revision: 1\n  schema: temper-qualification-model-artifact/v1", "target:\n  id: fake-coder-artifact\n  revision: 1\n  schema: temper-qualification-engine/v1", 1), want: "field declared_download_bytes not found"},
		{name: "uninjected product source identity", input: strings.Replace(canonical, "kind: product-promotion", "id: forged-packet\n      kind: product-promotion", 1), want: "must not supply its injected identity"},
		{name: "unsupported accepted claim", input: strings.Replace(canonical, "- artifact-identity\n  confounds:", "- artifact-quality\n  confounds:", 1), want: "unsupported claim"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseProductPromotionPacket([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseProductPromotionPacket() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileProductPromotionRefusesPrivateProjectionAndUnusedInputs(t *testing.T) {
	canonical := string(readProductPromotionFixture(t))
	privateCandidate := strings.Replace(
		canonical,
		"summary: Fake byte identities for exercising the C8 writer boundary",
		"summary: fixtures/private/fake-artifact-review.json",
		1,
	)
	if _, err := qualification.CompileProductPromotion([]byte(privateCandidate), qualification.ProductPromotionInputs{}); err == nil || !strings.Contains(err.Error(), "private or restricted locator") {
		t.Fatalf("private candidate CompileProductPromotion() error = %v", err)
	}

	unused := readProductPromotionProfileFixture(t)
	if _, err := qualification.CompileProductPromotion([]byte(canonical), qualification.ProductPromotionInputs{Profiles: [][]byte{unused}}); err == nil || !strings.Contains(err.Error(), "unused document") {
		t.Fatalf("unused input CompileProductPromotion() error = %v", err)
	}
}

func readProductPromotionFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/product-promotion.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readProductPromotionProfileFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/product-promotion-profile.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}
