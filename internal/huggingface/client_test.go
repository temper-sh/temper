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
