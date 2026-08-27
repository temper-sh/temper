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

func TestReferenceValidationAcceptsEveryQualificationDocumentSchema(t *testing.T) {
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

func TestLoadCatalogVerifiesExactModelArtifactAndBucketReferences(t *testing.T) {
	index := parseCatalogFixture(t)
	artifact := parseModelArtifactFixture(t)
	artifact.Applicability.MachineBuckets = []qualification.Reference{index.MachineBuckets[0].Document}
	artifactData, err := qualification.MarshalModelArtifactProfile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	index.Profiles = []qualification.IndexedDocument{{
		Document: qualification.Reference{
			Schema: qualification.ModelArtifactSchemaV1, ID: "example-coder-artifact", Revision: 1, SHA256: qualification.Digest(artifactData),
		},
		Path: "profiles/model-artifact/example-coder-artifact/1.yaml",
	}}
	files := map[string][]byte{
		exampleBucketPath:      readMachineBucketFixture(t),
		index.Profiles[0].Path: artifactData,
	}

	catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.ModelArtifacts) != 1 || catalog.ModelArtifacts[0].ID != "example-coder-artifact" {
		t.Fatalf("model artifacts = %#v", catalog.ModelArtifacts)
	}
}

func TestLoadCatalogVerifiesExactEngineAndBucketReferences(t *testing.T) {
	index := parseCatalogFixture(t)
	engine := parseEngineFixture(t)
	engine.Applicability.MachineBuckets = []qualification.Reference{index.MachineBuckets[0].Document}
	engineData, err := qualification.MarshalEngineProfile(engine)
	if err != nil {
		t.Fatal(err)
	}
	index.Profiles = []qualification.IndexedDocument{{
		Document: qualification.Reference{
			Schema: qualification.EngineSchemaV1, ID: engine.ID, Revision: engine.Revision, SHA256: qualification.Digest(engineData),
		},
		Path: "profiles/engine/example-local-engine/1.yaml",
	}}
	files := map[string][]byte{
		exampleBucketPath:      readMachineBucketFixture(t),
		index.Profiles[0].Path: engineData,
	}

	catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Engines) != 1 || catalog.Engines[0].ID != "example-local-engine" {
		t.Fatalf("engines = %#v", catalog.Engines)
	}
}

func TestLoadCatalogVerifiesExactModelRuntimeComposition(t *testing.T) {
	index, files := catalogWithModelRuntime(t, readModelRuntimeFixture(t))

	catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.ModelArtifacts) != 1 || len(catalog.Engines) != 1 || len(catalog.ModelRuntimes) != 1 {
		t.Fatalf("loaded profile counts = artifacts %d, engines %d, runtimes %d", len(catalog.ModelArtifacts), len(catalog.Engines), len(catalog.ModelRuntimes))
	}
	if catalog.ModelRuntimes[0].ID != "example-coder-runtime" {
		t.Fatalf("model runtimes = %#v", catalog.ModelRuntimes)
	}
}

func TestLoadCatalogRefusesModelRuntimeDependencyAbsentFromIndex(t *testing.T) {
	runtime := parseModelRuntimeFixture(t)
	runtime.Spec.ArtifactProfile.SHA256 = strings.Repeat("f", 64)
	runtime.Dependencies[0].Profile = runtime.Spec.ArtifactProfile
	runtimeData, err := qualification.MarshalModelRuntimeProfile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	index, files := catalogWithModelRuntime(t, runtimeData)

	_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err == nil || !strings.Contains(err.Error(), "references model artifact") || !strings.Contains(err.Error(), "absent from the index") {
		t.Fatalf("LoadCatalog() error = %v, want missing-artifact refusal", err)
	}
}

func TestLoadCatalogRefusesUnsupportedDrafterComposition(t *testing.T) {
	runtime := parseModelRuntimeFixture(t)
	runtime.Spec.Layout.Speculation = qualification.RuntimeSpeculation{
		State: "drafter", MethodRevision: "draft/v1", Sidecar: "sidecars/drafter.gguf", DraftTokens: 4,
	}
	runtimeData, err := qualification.MarshalModelRuntimeProfile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	index, files := catalogWithModelRuntime(t, runtimeData)

	_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err == nil || !strings.Contains(err.Error(), "requires engine capability \"drafter-speculation\"") {
		t.Fatalf("LoadCatalog() error = %v, want unsupported-drafter refusal", err)
	}
}

func TestLoadCatalogVerifiesExactToolProfile(t *testing.T) {
	index := parseCatalogFixture(t)
	toolData := readToolFixture(t)
	index.Profiles = []qualification.IndexedDocument{{
		Document: qualification.Reference{
			Schema: qualification.ToolSchemaV1, ID: "example-project-search", Revision: 1, SHA256: qualification.Digest(toolData),
		},
		Path: "profiles/tool/example-project-search/1.yaml",
	}}
	files := map[string][]byte{
		exampleBucketPath:      readMachineBucketFixture(t),
		index.Profiles[0].Path: toolData,
	}

	catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tools) != 1 || catalog.Tools[0].ID != "example-project-search" {
		t.Fatalf("tools = %#v", catalog.Tools)
	}
}

func TestLoadCatalogVerifiesExactModeComposition(t *testing.T) {
	index, files := catalogWithMode(t, readModeFixture(t))

	catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Modes) != 1 || catalog.Modes[0].ID != "example-local-search-mode" {
		t.Fatalf("modes = %#v", catalog.Modes)
	}
}

func TestLoadCatalogVerifiesExactActivityNarrowing(t *testing.T) {
	index, files := catalogWithActivity(t, readActivityFixture(t))

	catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Activities) != 1 || catalog.Activities[0].ID != "example-inspect-activity" {
		t.Fatalf("activities = %#v", catalog.Activities)
	}
	if len(catalog.Modes) != 1 || len(catalog.Tools) != 1 || len(catalog.ModelRuntimes) != 1 {
		t.Fatalf("loaded dependency counts = modes %d, tools %d, runtimes %d", len(catalog.Modes), len(catalog.Tools), len(catalog.ModelRuntimes))
	}
}

func TestLoadCatalogRefusesActivityModeAbsentFromIndex(t *testing.T) {
	activity := parseActivityFixture(t)
	activity.Spec.ModeProfile.SHA256 = strings.Repeat("f", 64)
	activity.Dependencies[0].Profile = activity.Spec.ModeProfile
	activityData, err := qualification.MarshalActivityProfile(activity)
	if err != nil {
		t.Fatal(err)
	}
	index, files := catalogWithActivity(t, activityData)

	_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err == nil || !strings.Contains(err.Error(), "references mode") || !strings.Contains(err.Error(), "absent from the index") {
		t.Fatalf("LoadCatalog() error = %v, want missing-mode refusal", err)
	}
}

func TestLoadCatalogRefusesActivityCompositionWidening(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ActivityProfile)
		want   string
	}{
		{name: "all mode tools", mutate: func(activity *qualification.ActivityProfile) {
			activity.Spec.ActiveTools = []qualification.Reference{parseModeFixture(t).Spec.Tools[0].Profile}
		}, want: "strict subset"},
		{name: "tool outside mode", mutate: func(activity *qualification.ActivityProfile) {
			tool := parseModeFixture(t).Spec.Tools[0].Profile
			tool.SHA256 = strings.Repeat("f", 64)
			activity.Spec.ActiveTools = []qualification.Reference{tool}
		}, want: "is not active in mode"},
		{name: "different roles", mutate: func(activity *qualification.ActivityProfile) {
			activity.Roles = []string{"rerank"}
		}, want: "roles must exactly match"},
		{name: "wider foreground", mutate: func(activity *qualification.ActivityProfile) {
			activity.Applicability.Foregrounds = []string{"harness"}
		}, want: "foreground applicability widens"},
		{name: "wider harness", mutate: func(activity *qualification.ActivityProfile) {
			activity.Applicability.Harnesses = []string{"other"}
		}, want: "harness applicability widens"},
		{name: "wider machine bucket", mutate: func(activity *qualification.ActivityProfile) {
			activity.Applicability.MachineBuckets = []qualification.Reference{parseCatalogFixture(t).MachineBuckets[0].Document}
		}, want: "machine-bucket applicability widens"},
		{name: "wider reads", mutate: func(activity *qualification.ActivityProfile) {
			activity.DataBoundary.Reads = []string{"local-request", "project-files"}
		}, want: "data boundary reads/writes"},
		{name: "wider network", mutate: func(activity *qualification.ActivityProfile) {
			activity.DataBoundary.Network = []qualification.ProfileNetworkUse{{Purpose: "tool-request", Destination: "example-provider", Timing: "request-time"}}
		}, want: "data boundary network"},
		{name: "different inference", mutate: func(activity *qualification.ActivityProfile) {
			activity.DataBoundary.Inference = "harness-owned-remote"
			activity.DataBoundary.Credentials = "harness-owned"
		}, want: "inference and credentials must exactly match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activity := parseActivityFixture(t)
			tt.mutate(&activity)
			activityData, err := qualification.MarshalActivityProfile(activity)
			if err != nil {
				t.Fatal(err)
			}
			index, files := catalogWithActivity(t, activityData)

			_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadCatalog() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogRefusesModeDependencyAbsentFromIndex(t *testing.T) {
	mode := parseModeFixture(t)
	mode.Spec.Tools[0].Profile.SHA256 = strings.Repeat("f", 64)
	mode.Dependencies[1].Profile = mode.Spec.Tools[0].Profile
	modeData, err := qualification.MarshalModeProfile(mode)
	if err != nil {
		t.Fatal(err)
	}
	index, files := catalogWithMode(t, modeData)

	_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
	if err == nil || !strings.Contains(err.Error(), "references tool") || !strings.Contains(err.Error(), "absent from the index") {
		t.Fatalf("LoadCatalog() error = %v, want missing-tool refusal", err)
	}
}

func TestLoadCatalogRefusesModeCompositionDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*qualification.ModeProfile)
		want   string
	}{
		{name: "active data boundary", mutate: func(mode *qualification.ModeProfile) { mode.DataBoundary.Reads = []string{"local-request"} }, want: "data boundary reads/writes"},
		{name: "harness transport revision", mutate: func(mode *qualification.ModeProfile) {
			mode.Spec.Harnesses[0].IntegrationRevision = "temper-pi-tools/v2"
		}, want: "has no exact harness transport"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode := parseModeFixture(t)
			tt.mutate(&mode)
			modeData, err := qualification.MarshalModeProfile(mode)
			if err != nil {
				t.Fatal(err)
			}
			index, files := catalogWithMode(t, modeData)

			_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadCatalog() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadCatalogRefusesProfileBucketAbsentFromIndex(t *testing.T) {
	artifact := parseModelArtifactFixture(t)
	artifact.Applicability.MachineBuckets = []qualification.Reference{{
		Schema: qualification.MachineBucketSchemaV1, ID: "another-bucket", Revision: 1, SHA256: strings.Repeat("a", 64),
	}}
	artifactData, err := qualification.MarshalModelArtifactProfile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	index := parseCatalogFixture(t)
	index.Profiles = []qualification.IndexedDocument{{
		Document: qualification.Reference{
			Schema: qualification.ModelArtifactSchemaV1, ID: artifact.ID, Revision: artifact.Revision, SHA256: qualification.Digest(artifactData),
		},
		Path: "profiles/model-artifact/example-coder-artifact/1.yaml",
	}}

	_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), map[string][]byte{
		exampleBucketPath:      readMachineBucketFixture(t),
		index.Profiles[0].Path: artifactData,
	})
	if err == nil || !strings.Contains(err.Error(), "absent from the index") {
		t.Fatalf("LoadCatalog() error = %v, want missing-bucket refusal", err)
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

func TestLoadCatalogAcceptsQualifiedDependencyClosure(t *testing.T) {
	tests := []struct {
		name      string
		lifecycle string
	}{
		{name: "experimental closure", lifecycle: qualification.LifecycleStatusExperimental},
		{name: "supported closure", lifecycle: qualification.LifecycleStatusSupported},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index, files := qualifiedCatalogBundle(t, tt.lifecycle)

			catalog, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
			if err != nil {
				t.Fatal(err)
			}
			if len(catalog.ModelRuntimes) != 1 || len(catalog.Modes) != 1 || len(catalog.Activities) != 1 {
				t.Fatalf("qualified composed counts = runtimes %d, modes %d, activities %d", len(catalog.ModelRuntimes), len(catalog.Modes), len(catalog.Activities))
			}
		})
	}
}

func TestLoadCatalogRefusesBrokenQualifiedDependencyClosure(t *testing.T) {
	bucket := parseCatalogFixture(t).MachineBuckets[0].Document

	t.Run("non-qualified dependency", func(t *testing.T) {
		artifact := parseModelArtifactFixture(t)
		artifactData := readModelArtifactFixture(t)
		engine, engineData := qualifiedEngineFixture(t, qualification.LifecycleStatusExperimental)
		runtime, runtimeData := qualifiedRuntimeFixture(
			t,
			qualification.LifecycleStatusExperimental,
			profileReference(artifact.ProfileEnvelope, artifactData),
			profileReference(engine.ProfileEnvelope, engineData),
			bucket,
		)
		index, files := catalogWithRuntimeMaterials(t, artifactData, engineData, runtimeData)

		_, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
		if err == nil || !strings.Contains(err.Error(), "to be QUALIFIED, got LAB") {
			t.Fatalf("LoadCatalog() error = %v, want non-qualified dependency refusal for %s", err, runtime.ID)
		}
	})

	t.Run("supported profile with experimental dependency", func(t *testing.T) {
		artifact, artifactData := qualifiedArtifactFixture(t, qualification.LifecycleStatusExperimental)
		engine, engineData := qualifiedEngineFixture(t, qualification.LifecycleStatusSupported)
		_, runtimeData := qualifiedRuntimeFixture(
			t,
			qualification.LifecycleStatusSupported,
			profileReference(artifact.ProfileEnvelope, artifactData),
			profileReference(engine.ProfileEnvelope, engineData),
			bucket,
		)
		index, files := catalogWithRuntimeMaterials(t, artifactData, engineData, runtimeData)

		_, err := qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
		if err == nil || !strings.Contains(err.Error(), "SUPPORTED profile requires dependency") || !strings.Contains(err.Error(), "got EXPERIMENTAL") {
			t.Fatalf("LoadCatalog() error = %v, want lifecycle-closure refusal", err)
		}
	})

	t.Run("evidence co-resident absent from index", func(t *testing.T) {
		artifact, artifactData := qualifiedArtifactFixture(t, qualification.LifecycleStatusExperimental)
		engine, engineData := qualifiedEngineFixture(t, qualification.LifecycleStatusExperimental)
		runtime, _ := qualifiedRuntimeFixture(
			t,
			qualification.LifecycleStatusExperimental,
			profileReference(artifact.ProfileEnvelope, artifactData),
			profileReference(engine.ProfileEnvelope, engineData),
			bucket,
		)
		runtime.Evidence[0].Scope.CoResidents = []qualification.ProfileCoResident{{
			RuntimeProfile: qualification.Reference{
				Schema: qualification.ModelRuntimeSchemaV1, ID: "missing-co-resident", Revision: 1, SHA256: strings.Repeat("a", 64),
			},
			Placement: "resident",
		}}
		key, err := qualification.EvidenceScopeKey(runtime.Evidence[0].Scope)
		if err != nil {
			t.Fatal(err)
		}
		runtime.Evidence[0].Scope.Key = key
		runtimeData, err := qualification.MarshalModelRuntimeProfile(runtime)
		if err != nil {
			t.Fatal(err)
		}
		index, files := catalogWithRuntimeMaterials(t, artifactData, engineData, runtimeData)

		_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
		if err == nil || !strings.Contains(err.Error(), "co-resident runtime missing-co-resident@1 absent from the index") {
			t.Fatalf("LoadCatalog() error = %v, want missing co-resident refusal", err)
		}
	})

	t.Run("qualified activity harness revision has no witness", func(t *testing.T) {
		index, files := qualifiedCatalogBundle(t, qualification.LifecycleStatusExperimental)
		activityPath := index.Profiles[0].Path
		activity, err := qualification.ParseActivityProfile(files[activityPath])
		if err != nil {
			t.Fatal(err)
		}
		activity.Evidence[0].Scope.Harnesses[0].IntegrationRevision = "temper-pi-tools/v2"
		key, err := qualification.EvidenceScopeKey(activity.Evidence[0].Scope)
		if err != nil {
			t.Fatal(err)
		}
		activity.Evidence[0].Scope.Key = key
		activityData, err := qualification.MarshalActivityProfile(activity)
		if err != nil {
			t.Fatal(err)
		}
		index.Profiles[0].Document.SHA256 = qualification.Digest(activityData)
		files[activityPath] = activityData

		_, err = qualification.LoadCatalog(marshalCatalogIndex(t, index), files)
		if err == nil || !strings.Contains(err.Error(), "qualified activity harness pi@temper-pi-tools/v1 has no exact evidence witness") {
			t.Fatalf("LoadCatalog() error = %v, want exact activity harness refusal", err)
		}
	})
}

func TestLoadCatalogRefusesRecommendationsUntilProjectionRulesAreImplemented(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "recommendation sets require performance and applicability cross-document validation") {
		t.Fatalf("LoadCatalog() error = %v, want recommendation refusal", err)
	}
}

func catalogWithRuntimeMaterials(t *testing.T, artifactData, engineData, runtimeData []byte) (qualification.CatalogIndex, map[string][]byte) {
	t.Helper()
	artifact, err := qualification.ParseModelArtifactProfile(artifactData)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := qualification.ParseEngineProfile(engineData)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := qualification.ParseModelRuntimeProfile(runtimeData)
	if err != nil {
		t.Fatal(err)
	}
	index := parseCatalogFixture(t)
	index.Profiles = []qualification.IndexedDocument{
		indexedProfile(engine.ProfileEnvelope, engineData, "engine"),
		indexedProfile(artifact.ProfileEnvelope, artifactData, "model-artifact"),
		indexedProfile(runtime.ProfileEnvelope, runtimeData, "model-runtime"),
	}
	files := map[string][]byte{
		exampleBucketPath:      readMachineBucketFixture(t),
		index.Profiles[0].Path: engineData,
		index.Profiles[1].Path: artifactData,
		index.Profiles[2].Path: runtimeData,
	}
	return index, files
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

func catalogWithModelRuntime(t *testing.T, runtimeData []byte) (qualification.CatalogIndex, map[string][]byte) {
	t.Helper()
	index := parseCatalogFixture(t)
	artifactData := readModelArtifactFixture(t)
	engineData := readEngineFixture(t)
	profiles := []qualification.IndexedDocument{
		{
			Document: qualification.Reference{Schema: qualification.EngineSchemaV1, ID: "example-local-engine", Revision: 1, SHA256: qualification.Digest(engineData)},
			Path:     "profiles/engine/example-local-engine/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModelArtifactSchemaV1, ID: "example-coder-artifact", Revision: 1, SHA256: qualification.Digest(artifactData)},
			Path:     "profiles/model-artifact/example-coder-artifact/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModelRuntimeSchemaV1, ID: "example-coder-runtime", Revision: 1, SHA256: qualification.Digest(runtimeData)},
			Path:     "profiles/model-runtime/example-coder-runtime/1.yaml",
		},
	}
	index.Profiles = profiles
	files := map[string][]byte{
		exampleBucketPath: readMachineBucketFixture(t),
		profiles[0].Path:  engineData,
		profiles[1].Path:  artifactData,
		profiles[2].Path:  runtimeData,
	}
	return index, files
}

func catalogWithMode(t *testing.T, modeData []byte) (qualification.CatalogIndex, map[string][]byte) {
	t.Helper()
	index := parseCatalogFixture(t)
	artifactData := readModelArtifactFixture(t)
	engineData := readEngineFixture(t)
	runtimeData := readModelRuntimeFixture(t)
	toolData := readToolFixture(t)
	profiles := []qualification.IndexedDocument{
		{
			Document: qualification.Reference{Schema: qualification.EngineSchemaV1, ID: "example-local-engine", Revision: 1, SHA256: qualification.Digest(engineData)},
			Path:     "profiles/engine/example-local-engine/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModeSchemaV1, ID: "example-local-search-mode", Revision: 1, SHA256: qualification.Digest(modeData)},
			Path:     "profiles/mode/example-local-search-mode/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModelArtifactSchemaV1, ID: "example-coder-artifact", Revision: 1, SHA256: qualification.Digest(artifactData)},
			Path:     "profiles/model-artifact/example-coder-artifact/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModelRuntimeSchemaV1, ID: "example-coder-runtime", Revision: 1, SHA256: qualification.Digest(runtimeData)},
			Path:     "profiles/model-runtime/example-coder-runtime/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ToolSchemaV1, ID: "example-project-search", Revision: 1, SHA256: qualification.Digest(toolData)},
			Path:     "profiles/tool/example-project-search/1.yaml",
		},
	}
	index.Profiles = profiles
	files := map[string][]byte{
		exampleBucketPath: readMachineBucketFixture(t),
		profiles[0].Path:  engineData,
		profiles[1].Path:  modeData,
		profiles[2].Path:  artifactData,
		profiles[3].Path:  runtimeData,
		profiles[4].Path:  toolData,
	}
	return index, files
}

func catalogWithActivity(t *testing.T, activityData []byte) (qualification.CatalogIndex, map[string][]byte) {
	t.Helper()
	index := parseCatalogFixture(t)
	artifactData := readModelArtifactFixture(t)
	engineData := readEngineFixture(t)
	runtimeData := readModelRuntimeFixture(t)
	toolData := readToolFixture(t)
	modeData := readModeFixture(t)
	profiles := []qualification.IndexedDocument{
		{
			Document: qualification.Reference{Schema: qualification.ActivitySchemaV1, ID: "example-inspect-activity", Revision: 1, SHA256: qualification.Digest(activityData)},
			Path:     "profiles/activity/example-inspect-activity/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.EngineSchemaV1, ID: "example-local-engine", Revision: 1, SHA256: qualification.Digest(engineData)},
			Path:     "profiles/engine/example-local-engine/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModeSchemaV1, ID: "example-local-search-mode", Revision: 1, SHA256: qualification.Digest(modeData)},
			Path:     "profiles/mode/example-local-search-mode/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModelArtifactSchemaV1, ID: "example-coder-artifact", Revision: 1, SHA256: qualification.Digest(artifactData)},
			Path:     "profiles/model-artifact/example-coder-artifact/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ModelRuntimeSchemaV1, ID: "example-coder-runtime", Revision: 1, SHA256: qualification.Digest(runtimeData)},
			Path:     "profiles/model-runtime/example-coder-runtime/1.yaml",
		},
		{
			Document: qualification.Reference{Schema: qualification.ToolSchemaV1, ID: "example-project-search", Revision: 1, SHA256: qualification.Digest(toolData)},
			Path:     "profiles/tool/example-project-search/1.yaml",
		},
	}
	index.Profiles = profiles
	files := map[string][]byte{
		exampleBucketPath: readMachineBucketFixture(t),
		profiles[0].Path:  activityData,
		profiles[1].Path:  engineData,
		profiles[2].Path:  modeData,
		profiles[3].Path:  artifactData,
		profiles[4].Path:  runtimeData,
		profiles[5].Path:  toolData,
	}
	return index, files
}
