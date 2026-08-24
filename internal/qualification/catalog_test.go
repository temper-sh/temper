package qualification_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/qualification"
)

const exampleBucketPath = "machine-buckets/example-gen5-32g-standard/1.yaml"

func TestParseCatalogIndexRoundTripsCanonicalFixture(t *testing.T) {
	data := readCatalogFixture(t)

	index, err := qualification.ParseCatalogIndex(data)
	if err != nil {
		t.Fatal(err)
	}
	if index.Schema != qualification.CatalogSchemaV1 || index.Revision != 1 || index.PublishedAt != "2026-08-25T12:00:00Z" {
		t.Fatalf("catalog index identity = %#v", index)
	}
	if len(index.MachineBuckets) != 1 || index.MachineBuckets[0].Path != exampleBucketPath {
		t.Fatalf("machine bucket index = %#v", index.MachineBuckets)
	}

	encoded, err := qualification.MarshalCatalogIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, data) {
		t.Fatalf("round trip changed canonical bytes\n got:\n%s\nwant:\n%s", encoded, data)
	}
}

func TestReferenceValidationAcceptsEveryC7DocumentSchema(t *testing.T) {
	schemas := []string{
		qualification.MachineBucketSchemaV1,
		qualification.ModelArtifactSchemaV1,
		qualification.EngineSchemaV1,
		qualification.ModelRuntimeSchemaV1,
		qualification.ToolSchemaV1,
		qualification.ModeSchemaV1,
		qualification.ActivitySchemaV1,
	}

	for _, schema := range schemas {
		t.Run(schema, func(t *testing.T) {
			reference := qualification.Reference{Schema: schema, ID: "example.document", Revision: 1, SHA256: strings.Repeat("a", 64)}
			if err := reference.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestReferenceValidationRefusesIncompleteOrOpenIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.Reference)
		want   string
	}{
		{name: "unknown schema", mutate: func(reference *qualification.Reference) { reference.Schema = "unknown/v1" }, want: "not a qualification document schema"},
		{name: "unstable id", mutate: func(reference *qualification.Reference) { reference.ID = "Example_Document" }, want: "lowercase stable id"},
		{name: "zero revision", mutate: func(reference *qualification.Reference) { reference.Revision = 0 }, want: "revision must be greater"},
		{name: "uppercase digest", mutate: func(reference *qualification.Reference) { reference.SHA256 = strings.Repeat("A", 64) }, want: "64 lowercase hexadecimal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reference := qualification.Reference{
				Schema: qualification.MachineBucketSchemaV1, ID: "example-document", Revision: 1, SHA256: strings.Repeat("a", 64),
			}
			tt.mutate(&reference)

			err := reference.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCatalogIndexValidationRefusesInvalidProjection(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.CatalogIndex)
		want   string
	}{
		{name: "unknown schema", mutate: func(index *qualification.CatalogIndex) { index.Schema = "unknown/v1" }, want: "schema is"},
		{name: "zero revision", mutate: func(index *qualification.CatalogIndex) { index.Revision = 0 }, want: "revision must be greater"},
		{name: "invalid publication instant", mutate: func(index *qualification.CatalogIndex) { index.PublishedAt = "yesterday" }, want: "must be RFC 3339"},
		{name: "no machine buckets", mutate: func(index *qualification.CatalogIndex) { index.MachineBuckets = nil }, want: "machine_buckets must not be empty"},
		{name: "wrong bucket schema", mutate: func(index *qualification.CatalogIndex) {
			index.MachineBuckets[0].Document.Schema = qualification.EngineSchemaV1
		}, want: "must be \"temper-qualification-machine-bucket/v1\""},
		{name: "derived bucket path mismatch", mutate: func(index *qualification.CatalogIndex) {
			index.MachineBuckets[0].Path = "machine-buckets/elsewhere.yaml"
		}, want: ".path is"},
		{name: "profile path mismatch", mutate: func(index *qualification.CatalogIndex) {
			index.Profiles = []qualification.IndexedDocument{{
				Document: qualification.Reference{Schema: qualification.EngineSchemaV1, ID: "example-engine", Revision: 2, SHA256: strings.Repeat("a", 64)},
				Path:     "profiles/engine/example-engine/latest.yaml",
			}}
		}, want: "profiles[0].path is"},
		{name: "semantic bucket duplicate", mutate: func(index *qualification.CatalogIndex) {
			duplicate := index.MachineBuckets[0]
			duplicate.Document.SHA256 = strings.Repeat("f", 64)
			index.MachineBuckets = append(index.MachineBuckets, duplicate)
		}, want: "repeats document identity"},
		{name: "unsorted buckets", mutate: func(index *qualification.CatalogIndex) {
			first := index.MachineBuckets[0]
			first.Document.ID = "aaa-example"
			first.Path = "machine-buckets/aaa-example/1.yaml"
			index.MachineBuckets = append(index.MachineBuckets, first)
		}, want: "sorted by exact document identity"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := parseCatalogFixture(t)
			tt.mutate(&index)

			_, err := qualification.MarshalCatalogIndex(index)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalCatalogIndex() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCatalogIndexAcceptsDerivedPathForEveryProfileKind(t *testing.T) {
	tests := []struct {
		schema string
		kind   string
	}{
		{schema: qualification.ModelArtifactSchemaV1, kind: "model-artifact"},
		{schema: qualification.EngineSchemaV1, kind: "engine"},
		{schema: qualification.ModelRuntimeSchemaV1, kind: "model-runtime"},
		{schema: qualification.ToolSchemaV1, kind: "tool"},
		{schema: qualification.ModeSchemaV1, kind: "mode"},
		{schema: qualification.ActivitySchemaV1, kind: "activity"},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			index := parseCatalogFixture(t)
			index.Profiles = []qualification.IndexedDocument{{
				Document: qualification.Reference{Schema: tt.schema, ID: "example-profile", Revision: 3, SHA256: strings.Repeat("a", 64)},
				Path:     "profiles/" + tt.kind + "/example-profile/3.yaml",
			}}

			if _, err := qualification.MarshalCatalogIndex(index); err != nil {
				t.Fatalf("MarshalCatalogIndex() error = %v", err)
			}
		})
	}
}

func TestParseCatalogIndexRefusesNoncanonicalOrAmbiguousYAML(t *testing.T) {
	canonical := string(readCatalogFixture(t))
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "unknown field",
			input: strings.Replace(canonical, "profiles: []", "extra: forbidden\nprofiles: []", 1),
			want:  "field extra not found",
		},
		{
			name: "alias",
			input: strings.Replace(
				strings.Replace(canonical, "revision: 1", "revision: &one 1", 1),
				"revision: 1\nschema: temper-qualification-catalog/v1", "revision: *one\nschema: temper-qualification-catalog/v1", 1,
			),
			want: "not canonical",
		},
		{
			name:  "duplicate key",
			input: strings.Replace(canonical, "profiles: []", "profiles: []\nprofiles: []", 1),
			want:  "mapping key \"profiles\" already defined",
		},
		{
			name:  "multiple documents",
			input: canonical + "---\nnull\n",
			want:  "multiple YAML documents",
		},
		{
			name:  "missing final newline",
			input: strings.TrimSuffix(canonical, "\n"),
			want:  "not canonical",
		},
		{
			name:  "noncanonical mapping order",
			input: "schema: temper-qualification-catalog/v1\n" + strings.Replace(canonical, "schema: temper-qualification-catalog/v1\n", "", 1),
			want:  "not canonical",
		},
		{
			name:  "noncanonical integer",
			input: strings.Replace(canonical, "revision: 1", "revision: 01", 1),
			want:  "not canonical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := qualification.ParseCatalogIndex([]byte(tt.input))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseCatalogIndex() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogVerifiesExactBucketBundle(t *testing.T) {
	files := map[string][]byte{
		exampleBucketPath:      readMachineBucketFixture(t),
		"history/ignored.yaml": []byte("not an indexed document\n"),
	}

	catalog, err := qualification.LoadCatalog(readCatalogFixture(t), files)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Index.Revision != 1 || len(catalog.MachineBuckets) != 1 {
		t.Fatalf("catalog = %#v", catalog)
	}
	bucket := catalog.MachineBuckets[0]
	if bucket.ID != "example-gen5-32g-standard" || bucket.Revision != 1 {
		t.Fatalf("loaded bucket identity = %#v", bucket)
	}
}

func TestLoadCatalogRefusesMissingOrMismatchedBucketBytes(t *testing.T) {
	t.Run("missing indexed file", func(t *testing.T) {
		_, err := qualification.LoadCatalog(readCatalogFixture(t), nil)
		if err == nil || !strings.Contains(err.Error(), "is missing") {
			t.Fatalf("LoadCatalog() error = %v, want missing-file refusal", err)
		}
	})

	t.Run("digest mismatch", func(t *testing.T) {
		files := map[string][]byte{exampleBucketPath: append(readMachineBucketFixture(t), '\n')}

		_, err := qualification.LoadCatalog(readCatalogFixture(t), files)
		if err == nil || !strings.Contains(err.Error(), "sha256 is") {
			t.Fatalf("LoadCatalog() error = %v, want digest refusal", err)
		}
	})

	t.Run("noncanonical document with matching digest", func(t *testing.T) {
		data := readMachineBucketFixture(t)
		noncanonical := []byte("schema: temper-qualification-machine-bucket/v1\n" + strings.Replace(string(data), "schema: temper-qualification-machine-bucket/v1\n", "", 1))
		index := parseCatalogFixture(t)
		index.MachineBuckets[0].Document.SHA256 = qualification.Digest(noncanonical)

		_, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), map[string][]byte{exampleBucketPath: noncanonical})
		if err == nil || !strings.Contains(err.Error(), "bucket bytes are not canonical") {
			t.Fatalf("LoadCatalog() error = %v, want canonical-byte refusal", err)
		}
	})

	t.Run("document identity mismatch", func(t *testing.T) {
		bucket := parseFixture(t)
		bucket.ID = "another-example-bucket"
		data, err := qualification.MarshalMachineBucket(bucket)
		if err != nil {
			t.Fatal(err)
		}
		index := parseCatalogFixture(t)
		index.MachineBuckets[0].Document.SHA256 = qualification.Digest(data)

		_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), map[string][]byte{exampleBucketPath: data})
		if err == nil || !strings.Contains(err.Error(), "identity is") {
			t.Fatalf("LoadCatalog() error = %v, want identity refusal", err)
		}
	})
}

func TestLoadCatalogRefusesUnimplementedProfileDocuments(t *testing.T) {
	index := parseCatalogFixture(t)
	index.Profiles = []qualification.IndexedDocument{{
		Document: qualification.Reference{Schema: qualification.EngineSchemaV1, ID: "example-engine", Revision: 1, SHA256: strings.Repeat("a", 64)},
		Path:     "profiles/engine/example-engine/1.yaml",
	}}

	_, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), map[string][]byte{exampleBucketPath: readMachineBucketFixture(t)})
	if err == nil || !strings.Contains(err.Error(), "profile documents are not implemented") {
		t.Fatalf("LoadCatalog() error = %v, want profile refusal", err)
	}
}

func TestLoadCatalogRefusesRecommendationsUntilProfilesAreImplemented(t *testing.T) {
	index := parseCatalogFixture(t)
	index.RecommendationSets = []qualification.RecommendationSet{{
		ID: "example-coding-options",
		Applicability: qualification.RecommendationApplicability{
			MachineBuckets: []qualification.Reference{index.MachineBuckets[0].Document},
			Foreground:     "local",
			Role:           "coding",
		},
		Explanation: "Example comparison without a preferred member",
		Members: []qualification.RecommendationMember{{
			RuntimeProfile: qualification.Reference{Schema: qualification.ModelRuntimeSchemaV1, ID: "example-runtime", Revision: 1, SHA256: strings.Repeat("b", 64)},
			Reason:         "Example evidence-backed tradeoff",
			Strengths:      []string{"Example first-attempt task success"},
			Costs:          []string{"Example memory cost"},
		}},
	}}

	_, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), map[string][]byte{exampleBucketPath: readMachineBucketFixture(t)})
	if err == nil || !strings.Contains(err.Error(), "recommendation sets require implemented profile documents") {
		t.Fatalf("LoadCatalog() error = %v, want recommendation refusal", err)
	}
}

func readCatalogFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readMachineBucketFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/machine-bucket.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parseCatalogFixture(t *testing.T) qualification.CatalogIndex {
	t.Helper()
	index, err := qualification.ParseCatalogIndex(readCatalogFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	return index
}

func marshalCatalogIndex(t *testing.T, index qualification.CatalogIndex) []byte {
	t.Helper()
	data, err := qualification.MarshalCatalogIndex(index)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
