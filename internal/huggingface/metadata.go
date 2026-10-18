package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Repository is one model repository at one commit.
type Repository struct {
	Commit string
	// License is the one the model card declares, or "".
	License string
	// Files are every file path at the commit.
	Files []string
}

// FileInfo is one file at a commit. SHA256 is set for files Hugging Face
// stores as large files; a small file has only a git hash and must be
// downloaded to be hashed.
type FileInfo struct {
	Path   string
	Bytes  int64
	SHA256 string
}

// Revision resolves a branch, tag or commit of a model repository.
func (c *Client) Revision(ctx context.Context, repo, revision string) (Repository, error) {
	if !repoPattern.MatchString(repo) {
		return Repository{}, fmt.Errorf("repository %q must be owner/name", repo)
	}
	if revision == "" || strings.ContainsAny(revision, "\r\n\x00") {
		return Repository{}, fmt.Errorf("revision %q is invalid", revision)
	}
	var value struct {
		SHA      string `json:"sha"`
		CardData struct {
			License any `json:"license"`
		} `json:"cardData"`
		Siblings []struct {
			Name string `json:"rfilename"`
		} `json:"siblings"`
	}
	endpoint := c.baseURL + "/api/models/" + escapePath(repo) + "/revision/" + url.PathEscape(revision)
	if err := c.readJSON(ctx, http.MethodGet, endpoint, nil, &value, "read "+repo+"@"+revision); err != nil {
		return Repository{}, err
	}
	if !revisionPattern.MatchString(value.SHA) {
		return Repository{}, fmt.Errorf("read %s@%s: Hugging Face returned no commit", repo, revision)
	}
	r := Repository{Commit: value.SHA}
	if license, ok := value.CardData.License.(string); ok {
		r.License = license
	}
	for _, sibling := range value.Siblings {
		r.Files = append(r.Files, sibling.Name)
	}
	return r, nil
}

// Files reads the size and, for large files, the SHA-256 of files at a commit.
func (c *Client) Files(ctx context.Context, repo, commit string, paths []string) ([]FileInfo, error) {
	if !repoPattern.MatchString(repo) || !revisionPattern.MatchString(commit) {
		return nil, fmt.Errorf("read %s: needs owner/name and a 40-character commit", repo)
	}
	form := url.Values{}
	for _, path := range paths {
		if err := validateFile(path); err != nil {
			return nil, fmt.Errorf("read %s: %w", repo, err)
		}
		form.Add("paths", path)
	}
	var entries []struct {
		Type string `json:"type"`
		Path string `json:"path"`
		Size int64  `json:"size"`
		LFS  *struct {
			OID  string `json:"oid"`
			Size int64  `json:"size"`
		} `json:"lfs"`
	}
	endpoint := c.baseURL + "/api/models/" + escapePath(repo) + "/paths-info/" + commit
	if err := c.readJSON(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()), &entries, "read files of "+repo); err != nil {
		return nil, err
	}
	found := map[string]FileInfo{}
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		info := FileInfo{Path: e.Path, Bytes: e.Size}
		if e.LFS != nil && hashPattern.MatchString(e.LFS.OID) {
			info.SHA256, info.Bytes = e.LFS.OID, e.LFS.Size
		}
		found[e.Path] = info
	}
	result := make([]FileInfo, 0, len(paths))
	for _, path := range paths {
		info, ok := found[path]
		if !ok || info.Bytes <= 0 {
			return nil, fmt.Errorf("%s has no file %q at %s", repo, path, commit[:7])
		}
		result = append(result, info)
	}
	return result, nil
}

func (c *Client) readJSON(ctx context.Context, method, endpoint string, body io.Reader, into any, operation string) error {
	request, err := c.request(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer response.Body.Close()
	if err := requireSuccess(response, operation); err != nil {
		return err
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(into); err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}
