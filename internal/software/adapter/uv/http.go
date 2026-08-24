package uv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
)

const MaxPythonMetadataBytes int64 = 32 * 1024 * 1024

var metadataPathPattern = regexp.MustCompile(`^/astral-sh/uv/0\.12\.[0-9]+/crates/uv-python/download-metadata\.json$`)

// HTTPReader performs the single version-matched managed-Python metadata GET.
// Redirects and caller-selected hosts are refused so a uv tag cannot silently
// move the trust boundary used to produce exact runtime artifacts.
type HTTPReader struct {
	client *http.Client
}

func NewHTTPReader(client *http.Client) (*HTTPReader, error) {
	if client == nil {
		return nil, errors.New("uv metadata HTTP client is required")
	}
	owned := *client
	owned.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPReader{client: &owned}, nil
}

func (r *HTTPReader) Read(ctx context.Context, locator string, limit int64) ([]byte, error) {
	if limit <= 0 || limit > MaxPythonMetadataBytes {
		return nil, fmt.Errorf("uv metadata byte limit must be between 1 and %d", MaxPythonMetadataBytes)
	}
	parsed, err := url.Parse(locator)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "raw.githubusercontent.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || !metadataPathPattern.MatchString(parsed.Path) {
		return nil, errors.New("uv metadata locator is not an approved version-matched release path")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
	if err != nil {
		return nil, fmt.Errorf("create uv metadata request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download uv metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download uv metadata: HTTP status %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("download uv metadata: content length %d exceeds %d bytes", response.ContentLength, limit)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download uv metadata: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download uv metadata: body exceeds %d bytes", limit)
	}
	return data, nil
}
