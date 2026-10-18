// Package huggingface adapts Hugging Face file downloads to Temper's immutable
// upstream read boundary.
package huggingface

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const defaultBaseURL = "https://huggingface.co"

var (
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type Config struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(config Config) (*Client, error) {
	baseURL := strings.TrimSuffix(config.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("Hugging Face base URL must be an absolute HTTP(S) origin")
	}
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{baseURL: baseURL, token: config.Token, http: client}, nil
}

func (c *Client) Open(ctx context.Context, repo, revision, file string) (io.ReadCloser, error) {
	if !repoPattern.MatchString(repo) {
		return nil, fmt.Errorf("download %q: repository must be owner/name", repo)
	}
	if !revisionPattern.MatchString(revision) {
		return nil, fmt.Errorf("download %s: revision must be a 40-character lowercase commit hash", repo)
	}
	if err := validateFile(file); err != nil {
		return nil, fmt.Errorf("download %s: %w", repo, err)
	}
	endpoint := c.baseURL + "/" + escapePath(repo) + "/resolve/" + revision + "/" + escapePath(file) + "?download=true"
	request, err := c.request(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download %s/%s: %w", repo, file, err)
	}
	if err := requireSuccess(response, "download "+repo+"/"+file); err != nil {
		response.Body.Close()
		return nil, err
	}
	return response.Body, nil
}

func (c *Client) request(ctx context.Context, method, endpoint string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("construct Hugging Face request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "temper/0")
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	return request, nil
}

func requireSuccess(response *http.Response, operation string) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		return fmt.Errorf("%s: HTTP %s", operation, response.Status)
	}
	return fmt.Errorf("%s: HTTP %s: %s", operation, response.Status, detail)
}

func escapePath(value string) string {
	parts := strings.Split(value, "/")
	for index := range parts {
		parts[index] = url.PathEscape(parts[index])
	}
	return strings.Join(parts, "/")
}

func validateFile(file string) error {
	if file == "" || strings.HasPrefix(file, "/") || strings.Contains(file, `\`) || strings.ContainsAny(file, "\r\n\x00") {
		return errors.New("file must be a safe repository-relative path")
	}
	parts := strings.Split(file, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errors.New("file must be a safe repository-relative path")
		}
	}
	return nil
}
