package huggingface_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/huggingface"
)

const (
	testRevision = "1111111111111111111111111111111111111111"
	testSHA      = "2222222222222222222222222222222222222222222222222222222222222222"
)

func TestResolveUsesMainMetadataAndLFSSHA256(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/models/owner/model" || request.URL.Query().Get("blobs") != "true" {
			t.Fatalf("request URL = %q, want model metadata with blobs", request.URL.String())
		}
		if got := request.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q", got)
		}
		io.WriteString(writer, `{"sha":"`+testRevision+`","siblings":[{"rfilename":"model/model.gguf","size":7,"lfs":{"sha256":"`+testSHA+`"}}]}`)
	}))
	defer server.Close()

	client, err := huggingface.New(huggingface.Config{BaseURL: server.URL, Token: "secret", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.Resolve(context.Background(), "owner/model", "model/model.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if pin.Revision != testRevision || pin.SHA256 != testSHA {
		t.Fatalf("pin = %#v", pin)
	}
}

func TestResolveRefusesFileWithoutLFSSHA256(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		io.WriteString(writer, `{"sha":"`+testRevision+`","siblings":[{"rfilename":"model.gguf","size":7}]}`)
	}))
	defer server.Close()
	client, err := huggingface.New(huggingface.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Resolve(context.Background(), "owner/model", "model.gguf")
	if err == nil || !strings.Contains(err.Error(), "no authoritative LFS SHA-256") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveSnapshotPinsLFSAndHashesSmallGitFilesAtOneRevision(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/models/owner/model":
			io.WriteString(writer, `{"sha":"`+testRevision+`","siblings":[{"rfilename":"config.json"},{"rfilename":"model.safetensors","lfs":{"sha256":"`+testSHA+`"}}]}`)
		case "/owner/model/resolve/" + testRevision + "/config.json":
			io.WriteString(writer, `{"model_type":"fixture"}`)
		default:
			t.Fatalf("unexpected request %q", request.URL.String())
		}
	}))
	defer server.Close()
	client, err := huggingface.New(huggingface.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	pin, err := client.ResolveSnapshot(context.Background(), "owner/model", []string{"config.json", "model.safetensors"})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(`{"model_type":"fixture"}`))
	wantSmall := hex.EncodeToString(digest[:])
	if pin.Revision != testRevision || len(pin.Files) != 2 || pin.Files[0].Name != "config.json" || pin.Files[0].SHA256 != wantSmall || pin.Files[1].SHA256 != testSHA {
		t.Fatalf("ResolveSnapshot() = %#v", pin)
	}
}

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
