// Package catalogsource provides read-only transports for signed software
// catalog publications. Verification and activation remain catalogupdate's
// responsibility.
package catalogsource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	publication "github.com/temper-sh/temper/internal/software/catalogpublication"
	"github.com/temper-sh/temper/internal/software/catalogupdate"
)

const (
	// ProductionChannelRoot is the reviewed public namespace for signed channel
	// publications. Changing it is a Temper release decision.
	ProductionChannelRoot = "https://temper-sh.github.io/temper/catalog/channels/"

	MaxChannelBytes   = 64 * 1024
	MaxCatalogBytes   = 8 * 1024 * 1024
	MaxSignatureBytes = 4 * 1024
)

// NewProductionHTTPS binds the reviewed public channel namespace. Callers may
// inject the HTTP client but cannot change the production source root.
func NewProductionHTTPS(client *http.Client) (*HTTPS, error) {
	return NewHTTPS(client, ProductionChannelRoot)
}

// HTTPS reads one configured channel namespace and immutable catalog
// publication directories. It owns no retry, cache, credentials, trust, or
// activation policy.
type HTTPS struct {
	client      *http.Client
	channelRoot *url.URL
}

func NewHTTPS(client *http.Client, channelRoot string) (*HTTPS, error) {
	if client == nil {
		return nil, errors.New("catalog HTTP client is required")
	}
	root, err := parseDirectoryURL(channelRoot, "catalog channel root")
	if err != nil {
		return nil, err
	}
	owned := *client
	callerRedirect := client.CheckRedirect
	owned.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("catalog read exceeded five redirects")
		}
		if err := validateHTTPSURL(request.URL); err != nil {
			return errors.New("catalog redirect must remain on absolute https without credentials, query, or fragment")
		}
		if callerRedirect != nil {
			return callerRedirect(request, via)
		}
		return nil
	}
	return &HTTPS{client: &owned, channelRoot: root}, nil
}

func (s *HTTPS) Channel(ctx context.Context, channel string) (catalogupdate.SignedArtifact, error) {
	if err := publication.ValidateChannelName(channel); err != nil {
		return catalogupdate.SignedArtifact{}, err
	}
	root := *s.channelRoot
	root.Path += channel + "/"
	return s.readPublication(ctx, &root, "channel", "channel.yaml", "channel.signature.yaml", MaxChannelBytes)
}

func (s *HTTPS) Catalog(ctx context.Context, locator string) (catalogupdate.SignedArtifact, error) {
	root, err := parseDirectoryURL(locator, "catalog locator")
	if err != nil {
		return catalogupdate.SignedArtifact{}, err
	}
	return s.readPublication(ctx, root, "software catalog", "catalog.yaml", "catalog.signature.yaml", MaxCatalogBytes)
}

func (s *HTTPS) readPublication(ctx context.Context, root *url.URL, label, dataName, signatureName string, dataLimit int64) (catalogupdate.SignedArtifact, error) {
	dataURL := *root
	dataURL.Path += dataName
	data, err := s.read(ctx, label, dataURL.String(), dataLimit)
	if err != nil {
		return catalogupdate.SignedArtifact{}, err
	}
	signatureURL := *root
	signatureURL.Path += signatureName
	signature, err := s.read(ctx, label+" signature", signatureURL.String(), MaxSignatureBytes)
	if err != nil {
		return catalogupdate.SignedArtifact{}, err
	}
	return catalogupdate.SignedArtifact{Data: data, Signature: signature}, nil
}

func (s *HTTPS) read(ctx context.Context, label, locator string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, locator, nil)
	if err != nil {
		return nil, fmt.Errorf("create %s request: %w", label, err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP status %d", label, response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("download %s: declared size %d exceeds %d-byte limit", label, response.ContentLength, limit)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", label, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: content exceeds %d-byte limit", label, limit)
	}
	return data, nil
}

func parseDirectoryURL(raw, label string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || validateHTTPSURL(parsed) != nil || !strings.HasSuffix(parsed.Path, "/") || parsed.RawPath != "" {
		return nil, fmt.Errorf("%s must be an absolute https directory URL without credentials, encoding, query, or fragment", label)
	}
	return parsed, nil
}

func validateHTTPSURL(parsed *url.URL) error {
	if parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("invalid https URL")
	}
	return nil
}

var _ catalogupdate.Source = (*HTTPS)(nil)
