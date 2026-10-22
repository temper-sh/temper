package presetcmd_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/catalog"
	"github.com/temper-sh/temper/internal/huggingface"
	"github.com/temper-sh/temper/internal/presetcmd"
	"github.com/temper-sh/temper/internal/userpreset"
)

const (
	q6Commit       = "4ca720788d1e01f1bff70c033e0d0028fd02e502"
	templateCommit = "855bffc49448e299789730ff92c9b8d834d6cc14"
	q6SHA          = "6666666666666666666666666666666666666666666666666666666666666666"
	templateText   = "{{ messages }}\n"
)

// hub serves recorded Hugging Face metadata: a large GGUF with its LFS
// SHA-256, and a small template that has only a git hash and is downloaded.
func hub(t *testing.T) *huggingface.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/models/unsloth/Qwen3.8-27B-GGUF/revision/main" || r.URL.Path == "/api/models/unsloth/Qwen3.8-27B-GGUF/revision/"+q6Commit:
			json.NewEncoder(w).Encode(map[string]any{"sha": q6Commit, "cardData": map[string]any{"license": "apache-2.0"},
				"siblings": []any{map[string]any{"rfilename": "Qwen3.8-27B-Q6_K.gguf"}, map[string]any{"rfilename": "Qwen3.8-27B-Q8_0.gguf"}}})
		case r.URL.Path == "/api/models/other/Qwen3.8-27B-GGUF/revision/main" || r.URL.Path == "/api/models/other/Qwen3.8-27B-GGUF/revision/"+q6Commit:
			json.NewEncoder(w).Encode(map[string]any{"sha": q6Commit, "cardData": map[string]any{"license": "apache-2.0"},
				"siblings": []any{map[string]any{"rfilename": "Qwen3.8-27B-Q6_K.gguf"}}})
		case strings.HasSuffix(r.URL.Path, "/paths-info/"+q6Commit) && r.Method == http.MethodPost:
			r.ParseForm()
			var entries []any
			for _, path := range r.PostForm["paths"] {
				if path == "Qwen3.8-27B-Q6_K.gguf" {
					entries = append(entries, map[string]any{"type": "file", "path": path, "size": 136, "lfs": map[string]any{"oid": q6SHA, "size": 22900000000}})
				}
			}
			json.NewEncoder(w).Encode(entries)
		case r.URL.Path == "/api/models/froggeric/Qwen-Fixed-Chat-Templates/revision/main":
			json.NewEncoder(w).Encode(map[string]any{"sha": templateCommit, "cardData": map[string]any{"license": "apache-2.0"}})
		case r.URL.Path == "/froggeric/Qwen-Fixed-Chat-Templates/resolve/"+templateCommit+"/chat_template.jinja":
			w.Write([]byte(templateText))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client, err := huggingface.New(huggingface.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func withHub(t *testing.T) (presetcmd.Command, string) {
	c, root := command(t)
	c.Hub = hub(t)
	return c, root
}

func read(t *testing.T, root, id string) catalog.UserPreset {
	t.Helper()
	raw, err := os.ReadFile(userpreset.Path(root, id))
	if err != nil {
		t.Fatal(err)
	}
	u, err := catalog.ParseUserPreset(id, raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestWeightsHelperRecordsExactFactsAndKeepsTheModel(t *testing.T) {
	c, root := withHub(t)
	if _, d, code := run(t, c, root, "fork", mtp, "qwen-q6"); code != 0 {
		t.Fatal(d)
	}
	out, diagnostics, code := run(t, c, root, "weights", "qwen-q6", "unsloth/Qwen3.8-27B-GGUF", "*Q6_K.gguf")
	if code != 0 || !strings.Contains(out, "qwen-q6 now uses weights qwen3.8-27b-q6-k") {
		t.Fatalf("weights: %d %s %s", code, out, diagnostics)
	}
	u := read(t, root, "qwen-q6")
	record := u.Artifacts["qwen3.8-27b-q6-k"]
	// The record carries Hugging Face's exact size and SHA-256; a wrong one
	// would only fail later, at download.
	if u.Changes["artifact"] != "qwen3.8-27b-q6-k" || record.SameModelAs != "qwen3.8-27b-q4xl" || record.Format != "gguf" || record.License != "apache-2.0" ||
		len(record.Files) != 1 || record.Files[0].Bytes != 22900000000 || record.Files[0].SHA256 != q6SHA || record.Sources[0].Revision != q6Commit {
		t.Fatalf("weights record: %+v", record)
	}
	if out, _, code := run(t, c, root, "check", "qwen-q6"); code != 0 || !strings.Contains(out, `artifact "qwen3.8-27b-q4xl" → "qwen3.8-27b-q6-k"`) {
		t.Fatalf("check: %s", out)
	}
	// Weights from another repository need the same-model claim.
	if _, diagnostics, code := run(t, c, root, "weights", "qwen-q6", "other/Qwen3.8-27B-GGUF", "Qwen3.8-27B-Q6_K.gguf", "--weights-id", "other-q6"); code == 0 || !strings.Contains(diagnostics, "--same-model-as qwen3.8-27b-q4xl") {
		t.Fatalf("other repository accepted without a claim: %s", diagnostics)
	}
	if _, diagnostics, code := run(t, c, root, "weights", "qwen-q6", "other/Qwen3.8-27B-GGUF", "Qwen3.8-27B-Q6_K.gguf", "--weights-id", "other-q6", "--same-model-as", "qwen3.8-27b-q4xl"); code != 0 {
		t.Fatalf("claimed weights refused: %s", diagnostics)
	}
	if _, diagnostics, code := run(t, c, root, "weights", "qwen-q6", "unsloth/Qwen3.8-27B-GGUF", "Missing.gguf"); code == 0 || !strings.Contains(diagnostics, `no file "Missing.gguf"`) {
		t.Fatalf("missing file accepted: %s", diagnostics)
	}
}

func TestTemplateHelperHashesTheTemplateAsPrepared(t *testing.T) {
	c, root := withHub(t)
	if _, d, code := run(t, c, root, "fork", mtp, "qwen-next"); code != 0 {
		t.Fatal(d)
	}
	out, diagnostics, code := run(t, c, root, "template", "qwen-next", "froggeric/Qwen-Fixed-Chat-Templates", "chat_template.jinja")
	if code != 0 || !strings.Contains(out, "qwen-next now uses template qwen-next-template") {
		t.Fatalf("template: %d %s %s", code, out, diagnostics)
	}
	u := read(t, root, "qwen-next")
	record := u.Templates["qwen-next-template"]
	sum := sha256.Sum256([]byte(templateText))
	if u.Changes["template"] != "qwen-next-template" || record.Files[0].SHA256 != hex.EncodeToString(sum[:]) || record.Files[0].Bytes != int64(len(templateText)) ||
		len(record.CompatibleArtifacts) != 1 || record.CompatibleArtifacts[0] != "qwen3.8-27b-q4xl" {
		t.Fatalf("template record: %+v", record)
	}
	if _, _, code := run(t, c, root, "template", "qwen-next", "builtin"); code != 0 || read(t, root, "qwen-next").Changes["template"] != catalog.BuiltinTemplate {
		t.Fatal("builtin template was not chosen")
	}
}

func TestLocalTemplateOptionNamesTheFileAndCompileReadsIt(t *testing.T) {
	c, root := command(t)
	if _, d, code := run(t, c, root, "fork", mtp, "qwen-mine"); code != 0 {
		t.Fatal(d)
	}
	if _, diagnostics, code := run(t, c, root, "template", "qwen-mine", "--local", "qwen-mine.jinja"); code == 0 || !strings.Contains(diagnostics, "qwen-mine.jinja") {
		t.Fatalf("a missing local file was accepted: %s", diagnostics)
	}
	if err := os.WriteFile(userpreset.Dir(root)+"/qwen-mine.jinja", []byte(templateText), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, diagnostics, code := run(t, c, root, "template", "qwen-mine", "--local", "qwen-mine.jinja"); code != 0 || !strings.Contains(out, "Each compile reads the file again") {
		t.Fatalf("local template: %d %s %s", code, out, diagnostics)
	}
	record := read(t, root, "qwen-mine").Templates["qwen-mine-template"]
	if record.Sources[0].Hub != catalog.HubLocal || record.Files[0].Path != "qwen-mine.jinja" || record.Files[0].SHA256 != "" {
		t.Fatalf("local record: %+v", record)
	}
	if out, _, code := run(t, c, root, "check", "qwen-mine"); code != 0 || !strings.Contains(out, `template "froggeric-qwen38" → "qwen-mine-template"`) {
		t.Fatalf("check: %s", out)
	}
	if _, diagnostics, code := run(t, c, root, "template", "qwen-mine", "--local", "other.json"); code == 0 || !strings.Contains(diagnostics, "not a .json file") {
		t.Fatalf("a .json template name was accepted: %s", diagnostics)
	}
}

func TestCopiesOfForksWithOwnRecordsCheckCleanly(t *testing.T) {
	c, root := withHub(t)
	for _, args := range [][]string{{"fork", mtp, "qwen-mine"}, {"fork", mtp, "qwen-q6"}, {"weights", "qwen-q6", "unsloth/Qwen3.8-27B-GGUF", "Qwen3.8-27B-Q6_K.gguf"}} {
		if _, d, code := run(t, c, root, args...); code != 0 {
			t.Fatal(args, d)
		}
	}
	if err := os.WriteFile(userpreset.Dir(root)+"/qwen-mine.jinja", []byte(templateText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, d, code := run(t, c, root, "template", "qwen-mine", "--local", "qwen-mine.jinja"); code != 0 {
		t.Fatal(d)
	}
	for _, source := range []string{"qwen-mine", "qwen-q6"} {
		if _, d, code := run(t, c, root, "copy", source, source+"-frozen"); code != 0 {
			t.Fatalf("copy of %s: %s", source, d)
		}
		out, _, code := run(t, c, root, "check", source+"-frozen")
		if code != 0 || strings.Contains(out, "Warning") {
			t.Fatalf("copy of %s: %d %s", source, code, out)
		}
	}
	if record := read(t, root, "qwen-mine-frozen").Templates["qwen-mine-template"]; record.Sources[0].Hub != catalog.HubLocal || record.Files[0].SHA256 != "" {
		t.Fatalf("the copy stopped reading the local file: %+v", record)
	}
}
