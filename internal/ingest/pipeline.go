package ingest

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sanjayrohith/redline/internal/cache"
)

// Uploader is the cache-side dependency CachePipeline streams into. It is
// satisfied by *cache.ChunkedUploader; the interface exists so the
// pipeline is testable without a real object store.
type Uploader interface {
	Upload(ctx context.Context, object string, src io.Reader) (*cache.UploadResult, error)
}

// ObjectRemover deletes a previously uploaded object. It is satisfied by
// *cache.Client.
type ObjectRemover interface {
	RemoveObject(ctx context.Context, key string) error
}

// CachePipeline composes fetch, streaming checksum verification, and
// upload into one pass: bytes flow from the upstream HTTP response
// straight into the object store, hashed as they go, with the full
// artifact never buffered in process memory.
type CachePipeline struct {
	httpClient *http.Client
	uploader   Uploader
	remover    ObjectRemover
}

// NewCachePipeline returns a CachePipeline using httpClient to fetch
// artifacts and uploader/remover to write them to (or discard them from)
// the object store.
func NewCachePipeline(httpClient *http.Client, uploader Uploader, remover ObjectRemover) *CachePipeline {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &CachePipeline{httpClient: httpClient, uploader: uploader, remover: remover}
}

// DownloadToCache fetches fileURL and streams it directly into the object
// store at objectKey, verifying its content against algorithm and
// expectedHexDigest as it streams. If the digest does not match once the
// stream is exhausted, the uploaded object is deleted and a
// *ChecksumMismatchError is returned - the pipeline never leaves a
// corrupted or tampered artifact live in the cache.
func (p *CachePipeline) DownloadToCache(ctx context.Context, fileURL, algorithm, expectedHexDigest, objectKey string, onProgress ProgressFunc) (*cache.UploadResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ingest: build download request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ingest: download %s: %w", fileURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ingest: unexpected status %d downloading %s", resp.StatusCode, fileURL)
	}

	h, err := newHasher(algorithm)
	if err != nil {
		return nil, err
	}

	reader := newProgressReader(io.TeeReader(resp.Body, h), onProgress)

	result, err := p.uploader.Upload(ctx, objectKey, reader)
	if err != nil {
		return nil, fmt.Errorf("ingest: stream %s to cache: %w", fileURL, err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expectedHexDigest) {
		if removeErr := p.remover.RemoveObject(ctx, objectKey); removeErr != nil {
			return nil, fmt.Errorf("%w (additionally failed to remove the mismatched object: %s)",
				&ChecksumMismatchError{Path: fileURL, Want: expectedHexDigest, Got: got}, removeErr)
		}
		return nil, &ChecksumMismatchError{Path: fileURL, Want: expectedHexDigest, Got: got}
	}

	return result, nil
}
