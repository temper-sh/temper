package catalogsource_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/catalogsource"
)

func TestHTTPSReadsExactChannelAndCatalogArtifacts(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"https://catalog.example/temper/channels/stable/channel.yaml":           "channel\n",
		"https://catalog.example/temper/channels/stable/channel.signature.yaml": "channel signature\n",
		"https://assets.example/catalogs/sha256-abc/catalog.yaml":               "catalog\n",
		"https://assets.example/catalogs/sha256-abc/catalog.signature.yaml":     "catalog signature\n",
	}
	var requests []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.String())
		body, ok := responses[request.URL.String()]
		if !ok {
			return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	source, err := catalogsource.NewHTTPS(client, "https://catalog.example/temper/channels/")
	if err != nil {
		t.Fatal(err)
	}

	channel, err := source.Channel(context.Background(), "stable")
	if err != nil {
		t.Fatalf("Channel() error = %v", err)
	}
	if string(channel.Data) != "channel\n" || string(channel.Signature) != "channel signature\n" {
		t.Fatalf("Channel() = data %q signature %q", channel.Data, channel.Signature)
	}
	catalog, err := source.Catalog(context.Background(), "https://assets.example/catalogs/sha256-abc/")
	if err != nil {
		t.Fatalf("Catalog() error = %v", err)
	}
	if string(catalog.Data) != "catalog\n" || string(catalog.Signature) != "catalog signature\n" {
		t.Fatalf("Catalog() = data %q signature %q", catalog.Data, catalog.Signature)
	}
	wantRequests := []string{
		"GET https://catalog.example/temper/channels/stable/channel.yaml",
		"GET https://catalog.example/temper/channels/stable/channel.signature.yaml",
		"GET https://assets.example/catalogs/sha256-abc/catalog.yaml",
		"GET https://assets.example/catalogs/sha256-abc/catalog.signature.yaml",
	}
	if strings.Join(requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Errorf("requests =\n%s\nwant:\n%s", strings.Join(requests, "\n"), strings.Join(wantRequests, "\n"))
	}
}

func TestHTTPSRefusesInvalidRootsChannelsAndCatalogLocatorsBeforeReads(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid input performed an HTTP read")
		return nil, nil
	})}
	invalidRoots := []string{
		"http://catalog.example/channels/",
		"https://user:secret@catalog.example/channels/",
		"https://catalog.example/channels/#fragment",
		"https://catalog.example/channels/?token=secret",
		"https://catalog.example/channels",
	}
	for _, root := range invalidRoots {
		if _, err := catalogsource.NewHTTPS(client, root); err == nil {
			t.Errorf("NewHTTPS(%q) succeeded", root)
		}
	}
	if _, err := catalogsource.NewHTTPS(nil, "https://catalog.example/channels/"); err == nil {
		t.Error("NewHTTPS(nil) succeeded")
	}

	source, err := catalogsource.NewHTTPS(client, "https://catalog.example/channels/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Channel(context.Background(), "../stable"); err == nil {
		t.Error("Channel(path traversal) succeeded")
	}
	invalidCatalogs := []string{
		"file:///tmp/catalog/",
		"https://user:secret@catalog.example/catalog/",
		"https://catalog.example/catalog/#fragment",
		"https://catalog.example/catalog/?token=secret",
		"https://catalog.example/catalog",
	}
	for _, locator := range invalidCatalogs {
		if _, err := source.Catalog(context.Background(), locator); err == nil {
			t.Errorf("Catalog(%q) succeeded", locator)
		}
	}
}

func TestHTTPSBoundsArtifactsAndStopsAfterTheFirstFailure(t *testing.T) {
	t.Parallel()

	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", catalogsource.MaxChannelBytes+1))),
			Header:     make(http.Header),
		}, nil
	})}
	source, err := catalogsource.NewHTTPS(client, "https://catalog.example/channels/")
	if err != nil {
		t.Fatal(err)
	}
	_, err = source.Channel(context.Background(), "stable")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Channel() error = %v, want bound refusal", err)
	}
	if requests != 1 {
		t.Errorf("oversized channel performed %d requests, want 1", requests)
	}
}

func TestHTTPSPropagatesCancellationStatusAndRedirectRefusals(t *testing.T) {
	t.Parallel()

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cancelSource, err := catalogsource.NewHTTPS(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}, "https://catalog.example/channels/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cancelSource.Channel(cancelled, "stable"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Channel(cancelled) error = %v", err)
	}

	statusSource, err := catalogsource.NewHTTPS(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header)}, nil
	})}, "https://catalog.example/channels/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := statusSource.Channel(context.Background(), "stable"); err == nil || !strings.Contains(err.Error(), "HTTP status 404") {
		t.Fatalf("Channel(404) error = %v", err)
	}

	redirectSource, err := catalogsource.NewHTTPS(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     http.Header{"Location": []string{"http://catalog.example/insecure"}},
		}, nil
	})}, "https://catalog.example/channels/")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := redirectSource.Channel(context.Background(), "stable"); err == nil || !strings.Contains(err.Error(), "redirect must remain") {
		t.Fatalf("Channel(insecure redirect) error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
