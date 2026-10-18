package huggingface_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/temper-sh/temper/internal/huggingface"
)

const (
	testRevision = "1111111111111111111111111111111111111111"
	testSHA      = "2222222222222222222222222222222222222222222222222222222222222222"
)

func TestOpenDownloadsExactRevision(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		want := "/owner/model/resolve/" + testRevision + "/nested/model.gguf"
		if request.URL.Path != want || request.URL.Query().Get("download") != "true" {
			t.Fatalf("request URL = %q, want %q?download=true", request.URL.String(), want)
		}
		io.WriteString(writer, "weights")
	}))
	defer server.Close()
	client, err := huggingface.New(huggingface.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := client.Open(context.Background(), "owner/model", testRevision, "nested/model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "weights" {
		t.Fatalf("download = %q", got)
	}
}

func TestMetadataGivesCommitLicenseSizesAndLargeFileHashes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/api/models/owner/model/revision/main":
			io.WriteString(writer, `{"sha": "`+testRevision+`", "cardData": {"license": "mit"}, "siblings": [{"rfilename": "model.gguf"}, {"rfilename": "config.json"}]}`)
		case request.URL.Path == "/api/models/owner/model/paths-info/"+testRevision && request.Method == http.MethodPost:
			request.ParseForm()
			if len(request.PostForm["paths"]) != 2 {
				t.Errorf("paths = %v", request.PostForm["paths"])
			}
			io.WriteString(writer, `[{"type": "file", "path": "model.gguf", "size": 134, "lfs": {"oid": "`+testSHA+`", "size": 9000}},
			  {"type": "file", "path": "config.json", "size": 42, "oid": "3fddda1850c5fd0c93e9ac3f7e20d8691b2163de"}]`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	client, err := huggingface.New(huggingface.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := client.Revision(context.Background(), "owner/model", "main")
	if err != nil || repository.Commit != testRevision || repository.License != "mit" || len(repository.Files) != 2 {
		t.Fatalf("revision = %+v, %v", repository, err)
	}
	files, err := client.Files(context.Background(), "owner/model", testRevision, []string{"model.gguf", "config.json"})
	if err != nil || files[0].Bytes != 9000 || files[0].SHA256 != testSHA || files[1].Bytes != 42 || files[1].SHA256 != "" {
		t.Fatalf("files = %+v, %v", files, err)
	}
	if _, err := client.Files(context.Background(), "owner/model", testRevision, []string{"model.gguf", "absent.gguf"}); err == nil {
		t.Fatal("an absent file was accepted")
	}
}
