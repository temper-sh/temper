// Package huggingface adapts the Hugging Face HTTP API to Temper's immutable
// upstream read boundary.
package huggingface

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/temper-sh/temper/internal/upstream"
)

const (
	defaultBaseURL = "https://huggingface.co"
	metadataLimit  = 32 << 20
	metadataWait   = 30 * time.Second
)

var (
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)
	revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
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

func (c *Client) Resolve(ctx context.Context, repo, file string) (upstream.FilePin, error) {
	if !repoPattern.MatchString(repo) {
		return upstream.FilePin{}, fmt.Errorf("resolve %q: repository must be owner/name", repo)
	}
	if err := validateFile(file); err != nil {
		return upstream.FilePin{}, fmt.Errorf("resolve %q: %w", repo, err)
	}

	metadataCtx, cancel := context.WithTimeout(ctx, metadataWait)
	defer cancel()
	endpoint := c.baseURL + "/api/models/" + escapePath(repo) + "?blobs=true"
	request, err := c.request(metadataCtx, http.MethodGet, endpoint)
	if err != nil {
		return upstream.FilePin{}, err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return upstream.FilePin{}, fmt.Errorf("resolve %s metadata: %w", repo, err)
	}
	defer response.Body.Close()
	if err := requireSuccess(response, "resolve "+repo+" metadata"); err != nil {
		return upstream.FilePin{}, err
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, metadataLimit+1))
	if err != nil {
		return upstream.FilePin{}, fmt.Errorf("read %s metadata: %w", repo, err)
	}
	if len(body) > metadataLimit {
		return upstream.FilePin{}, fmt.Errorf("read %s metadata: response exceeds %d bytes", repo, metadataLimit)
	}
	var metadata modelInfo
	if err := json.Unmarshal(body, &metadata); err != nil {
		return upstream.FilePin{}, fmt.Errorf("decode %s metadata: %w", repo, err)
	}
	if !revisionPattern.MatchString(metadata.SHA) {
		return upstream.FilePin{}, fmt.Errorf("resolve %s: upstream returned an invalid main revision", repo)
	}

	var selected *sibling
	for index := range metadata.Siblings {
		if metadata.Siblings[index].Name == file {
			if selected != nil {
				return upstream.FilePin{}, fmt.Errorf("resolve %s/%s: metadata repeats the selected file", repo, file)
			}
			selected = &metadata.Siblings[index]
		}
	}
	if selected == nil {
		return upstream.FilePin{}, fmt.Errorf("resolve %s/%s: selected file is absent at main", repo, file)
	}
	if selected.LFS == nil || !sha256Pattern.MatchString(selected.LFS.SHA256) {
		return upstream.FilePin{}, fmt.Errorf("resolve %s/%s: selected file has no authoritative LFS SHA-256", repo, file)
	}
	return upstream.FilePin{Revision: metadata.SHA, SHA256: selected.LFS.SHA256}, nil
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
	request, err := c.request(ctx, http.MethodGet, endpoint)
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

type modelInfo struct {
	SHA      string    `json:"sha"`
	Siblings []sibling `json:"siblings"`
}

type sibling struct {
	Name string   `json:"rfilename"`
	LFS  *lfsInfo `json:"lfs"`
}

type lfsInfo struct {
	SHA256 string `json:"sha256"`
}

func (c *Client) request(ctx context.Context, method, endpoint string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
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
