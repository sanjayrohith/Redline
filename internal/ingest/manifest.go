package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// ErrRepositoryNotFound is returned when the upstream host has no
// repository at the requested reference.
var ErrRepositoryNotFound = errors.New("ingest: repository not found")

// ManifestFile is one file's metadata from the upstream repository, known
// without downloading its content.
type ManifestFile struct {
	Path string
	Size int64
	OID  string
}

// Manifest is a repository's file listing and revision identity, enough
// to make an ingestion decision without ever fetching a weight byte.
type Manifest struct {
	RevisionSHA string
	Files       []ManifestFile
}

// Client fetches repository manifests from the upstream host.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the client used for upstream requests.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// WithBaseURL overrides the upstream host, for testing against a fake server.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// NewClient returns a Client configured with opts, defaulting to the real
// huggingface.co API over http.DefaultClient.
func NewClient(opts ...Option) *Client {
	c := &Client{httpClient: http.DefaultClient, baseURL: "https://huggingface.co"}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type treeEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	OID  string `json:"oid"`
}

type revisionInfo struct {
	SHA string `json:"sha"`
}

// FetchManifest queries the upstream repository API for ref's file listing
// and revision SHA, making no request for any file's content.
func (c *Client) FetchManifest(ctx context.Context, ref Reference) (*Manifest, error) {
	sha, err := c.fetchRevisionSHA(ctx, ref)
	if err != nil {
		return nil, err
	}

	files, err := c.fetchFileTree(ctx, ref)
	if err != nil {
		return nil, err
	}

	return &Manifest{RevisionSHA: sha, Files: files}, nil
}

// FetchConfig fetches and decodes a repository's config.json, returning
// nil without error if the repository has none - config.json is
// conventional, not guaranteed, so its absence falls back to tensor-name
// architecture inference rather than failing ingestion outright.
func (c *Client) FetchConfig(ctx context.Context, ref Reference) (*ModelConfig, error) {
	endpoint := fmt.Sprintf("%s/%s/resolve/%s/config.json", c.baseURL, ref.RepoID(), url.PathEscape(ref.Revision))

	var cfg ModelConfig
	if err := c.getJSON(ctx, endpoint, &cfg); err != nil {
		if errors.Is(err, ErrRepositoryNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

// FileURL returns the direct download URL for path within ref's
// repository, in the same "resolve" form FetchConfig itself fetches from.
func (c *Client) FileURL(ref Reference, path string) string {
	return fmt.Sprintf("%s/%s/resolve/%s/%s", c.baseURL, ref.RepoID(), url.PathEscape(ref.Revision), path)
}

type repoCardData struct {
	License string `json:"license"`
}

type repoInfo struct {
	CardData repoCardData `json:"cardData"`
}

// FetchLicense returns the repository's declared license identifier (as
// SPDX-style tags such as "apache-2.0" or "mit"), or "" if the repository
// declares none - a missing license is common enough on upstream hosts
// that it must not fail ingestion outright.
func (c *Client) FetchLicense(ctx context.Context, ref Reference) (string, error) {
	endpoint := fmt.Sprintf("%s/api/models/%s", c.baseURL, ref.RepoID())

	var info repoInfo
	if err := c.getJSON(ctx, endpoint, &info); err != nil {
		if errors.Is(err, ErrRepositoryNotFound) {
			return "", nil
		}
		return "", err
	}
	return info.CardData.License, nil
}

func (c *Client) fetchRevisionSHA(ctx context.Context, ref Reference) (string, error) {
	endpoint := fmt.Sprintf("%s/api/models/%s/revision/%s", c.baseURL, ref.RepoID(), url.PathEscape(ref.Revision))

	var info revisionInfo
	if err := c.getJSON(ctx, endpoint, &info); err != nil {
		return "", err
	}
	if info.SHA == "" {
		return "", fmt.Errorf("ingest: revision response for %s did not include a sha", ref.RepoID())
	}
	return info.SHA, nil
}

func (c *Client) fetchFileTree(ctx context.Context, ref Reference) ([]ManifestFile, error) {
	endpoint := fmt.Sprintf("%s/api/models/%s/tree/%s?recursive=true", c.baseURL, ref.RepoID(), url.PathEscape(ref.Revision))

	var entries []treeEntry
	if err := c.getJSON(ctx, endpoint, &entries); err != nil {
		return nil, err
	}

	files := make([]ManifestFile, 0, len(entries))
	for _, e := range entries {
		if e.Type != "file" {
			continue
		}
		files = append(files, ManifestFile{Path: e.Path, Size: e.Size, OID: e.OID})
	}
	return files, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("ingest: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ingest: request %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return ErrRepositoryNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest: unexpected status %d from %s", resp.StatusCode, endpoint)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("ingest: decode response from %s: %w", endpoint, err)
	}
	return nil
}
