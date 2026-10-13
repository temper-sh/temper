package upstreamrelease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPReaderPerformsOneContextBoundHTTPSGet(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.String() != "https://example.invalid/release.tar.gz" {
			t.Fatalf("request = %s %s", request.Method, request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("archive")), Header: make(http.Header)}, nil
	})}
	reader, err := NewHTTPReader(client)
	if err != nil {
		t.Fatal(err)
	}
	body, err := reader.Open(context.Background(), "https://example.invalid/release.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	if err != nil || body.Close() != nil || string(data) != "archive" || calls != 1 {
		t.Fatalf("body = %q, calls = %d, error = %v", data, calls, err)
	}
}

func TestHTTPReaderRejectsInvalidLocatorAndNonSuccess(t *testing.T) {
	if _, err := NewHTTPReader(nil); err == nil {
		t.Fatal("NewHTTPReader(nil) succeeded")
	}
	reader, _ := NewHTTPReader(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("missing")), Header: make(http.Header)}, nil
	})})
	if _, err := reader.Open(context.Background(), "file:///tmp/archive"); err == nil {
		t.Fatal("Open(file URL) succeeded")
	}
	if _, err := reader.Open(context.Background(), "https://example.invalid/missing"); err == nil || !strings.Contains(err.Error(), "HTTP status 404") {
		t.Fatalf("Open(404) error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	failing, _ := NewHTTPReader(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})})
	if _, err := failing.Open(cancelled, "https://example.invalid/archive"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Open(cancelled) error = %v", err)
	}

	redirecting, _ := NewHTTPReader(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{"Location": []string{"http://example.invalid/insecure"}}}, nil
	})})
	if _, err := redirecting.Open(context.Background(), "https://example.invalid/archive"); err == nil || !strings.Contains(err.Error(), "redirect must remain") {
		t.Fatalf("Open(insecure redirect) error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
