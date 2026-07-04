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

// Quarantiner moves a corrupted object out of its normal object-key space
// for later operator inspection, then removes it from that space so
// nothing can read it from its original location. It is satisfied by
// *cache.Client.
type Quarantiner interface {
	Quarantine(ctx context.Context, key string) (quarantineKey string, err error)
}

// CachePipeline composes fetch, streaming checksum verification, and
// upload into one pass: bytes flow from the upstream HTTP response
// straight into the object store, hashed as they go, with the full
// artifact never buffered in process memory.
type CachePipeline struct {
	httpClient  *http.Client
	uploader    Uploader
	quarantiner Quarantiner
}

// NewCachePipeline returns a CachePipeline using httpClient to fetch
// artifacts, uploader to write them to the object store, and quarantiner
// to move aside (never silently discard) anything that fails verification.
func NewCachePipeline(httpClient *http.Client, uploader Uploader, quarantiner Quarantiner) *CachePipeline {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &CachePipeline{httpClient: httpClient, uploader: uploader, quarantiner: quarantiner}
}

// DownloadToCache fetches fileURL and streams it directly into the object
// store at objectKey, verifying its content against algorithm and
// expectedHexDigest as it streams. If the digest does not match once the
// stream is exhausted, the uploaded object is quarantined - moved aside
// for operator inspection, then purged from its normal location - and a
// *ChecksumMismatchError is returned - the pipeline never leaves a
// corrupted or tampered artifact live in the cache, and never silently
// deletes evidence of what actually went wrong either.
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
		mismatch := &ChecksumMismatchError{Path: fileURL, Want: expectedHexDigest, Got: got}
		quarantineKey, quarantineErr := p.quarantiner.Quarantine(ctx, objectKey)
		if quarantineErr != nil {
			return nil, fmt.Errorf("%w (additionally failed to quarantine the mismatched object: %s)", mismatch, quarantineErr)
		}
		mismatch.QuarantineKey = quarantineKey
		return nil, mismatch
	}

	return result, nil
}
