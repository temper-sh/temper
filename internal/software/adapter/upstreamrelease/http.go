package upstreamrelease

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ArtifactReader is the only installation transport edge. Tests supply an
// offline reader; the production edge below performs one context-bound GET and
// owns no retry, cache, release lookup, or credential discovery.
type ArtifactReader interface {
	Open(context.Context, string) (io.ReadCloser, error)
}

type HTTPReader struct{ client *http.Client }

func NewHTTPReader(client *http.Client) (*HTTPReader, error) {
	if client == nil {
		return nil, errors.New("release artifact HTTP client is required")
	}
	owned := *client
	callerRedirect := client.CheckRedirect
	owned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("release artifact download exceeded five redirects")
		}
		parsed := request.URL
		if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
			return errors.New("release artifact redirect must remain on absolute https")
		}
		if callerRedirect != nil {
			return callerRedirect(request, via)
		}
		return nil
	}
	return &HTTPReader{client: &owned}, nil
}

func (r *HTTPReader) Open(ctx context.Context, locator string) (io.ReadCloser, error) {
	parsed, err := url.Parse(locator)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("release artifact locator must be an absolute https URL without credentials or fragment")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
	if err != nil {
		return nil, fmt.Errorf("create release artifact request: %w", err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download release artifact: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("download release artifact: HTTP status %d", response.StatusCode)
	}
	return response.Body, nil
}
