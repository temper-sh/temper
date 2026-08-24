package uv_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/temper-sh/temper/internal/software/adapter/uv"
)

const approvedMetadataLocator = "https://raw.githubusercontent.com/astral-sh/uv/0.12.5/crates/uv-python/download-metadata.json"

func TestHTTPReaderPerformsOneBoundedVersionMatchedHTTPSGet(t *testing.T) {
	var requests []*http.Request
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request)
		return response(http.StatusOK, `{ "entry": {} }`), nil
	})}
	reader, err := uv.NewHTTPReader(client)
	if err != nil {
		t.Fatal(err)
	}
	data, err := reader.Read(context.Background(), approvedMetadataLocator, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{ "entry": {} }` || len(requests) != 1 || requests[0].Method != http.MethodGet || requests[0].URL.String() != approvedMetadataLocator || requests[0].Header.Get("Accept") != "application/json" {
		t.Fatalf("data/requests = %q / %#v", data, requests)
	}
}

func TestHTTPReaderRefusesUnapprovedLocatorAndLimitBeforeReading(t *testing.T) {
	reads := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		reads++
		return response(http.StatusOK, "{}"), nil
	})}
	if _, err := uv.NewHTTPReader(nil); err == nil {
		t.Fatal("NewHTTPReader(nil) succeeded")
	}
	reader, err := uv.NewHTTPReader(client)
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		"http://raw.githubusercontent.com/astral-sh/uv/0.12.5/crates/uv-python/download-metadata.json",
		"https://user:secret@raw.githubusercontent.com/astral-sh/uv/0.12.5/crates/uv-python/download-metadata.json",
		"https://raw.githubusercontent.com/astral-sh/uv/main/crates/uv-python/download-metadata.json",
		"https://example.invalid/astral-sh/uv/0.12.5/crates/uv-python/download-metadata.json",
		approvedMetadataLocator + "?token=secret",
	}
	for _, locator := range invalid {
		if _, err := reader.Read(context.Background(), locator, 1024); err == nil {
			t.Errorf("Read(%q) succeeded", locator)
		}
	}
	for _, limit := range []int64{0, uv.MaxPythonMetadataBytes + 1} {
		if _, err := reader.Read(context.Background(), approvedMetadataLocator, limit); err == nil {
			t.Errorf("Read(limit=%d) succeeded", limit)
		}
	}
	if reads != 0 {
		t.Fatalf("invalid request performed %d reads", reads)
	}
}

func TestHTTPReaderBoundsBodyAndRefusesStatusAndRedirect(t *testing.T) {
	tests := []struct {
		name string
		body string
		code int
		want string
	}{
		{name: "body", body: "12345", code: http.StatusOK, want: "body exceeds"},
		{name: "status", body: "missing", code: http.StatusNotFound, want: "HTTP status 404"},
		{name: "redirect", body: "", code: http.StatusFound, want: "HTTP status 302"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests++
				result := response(test.code, test.body)
				if test.code == http.StatusFound {
					result.Header.Set("Location", "https://example.invalid/moved")
				}
				return result, nil
			})}
			reader, err := uv.NewHTTPReader(client)
			if err != nil {
				t.Fatal(err)
			}
			_, err = reader.Read(context.Background(), approvedMetadataLocator, 4)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if requests != 1 {
				t.Fatalf("requests = %d", requests)
			}
		})
	}
}

func TestHTTPReaderPropagatesContextCancellation(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})}
	reader, err := uv.NewHTTPReader(client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.Read(ctx, approvedMetadataLocator, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
