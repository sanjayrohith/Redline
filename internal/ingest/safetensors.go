package ingest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrHeaderTooLarge marks a safetensors header length prefix claiming a
// size beyond the configured sanity bound, before any bytes of the header
// itself are fetched.
var ErrHeaderTooLarge = errors.New("ingest: safetensors header length exceeds sanity bound")

// DefaultMaxHeaderBytes bounds the safetensors JSON header size this
// gateway will ever request or decode. Real headers are kilobytes to a
// few megabytes even for enormous models; 64MiB is generous headroom
// against a crafted length prefix driving an unbounded fetch.
const DefaultMaxHeaderBytes = 64 * 1024 * 1024

// RangeFetcher fetches the inclusive byte range [start, end] of a remote
// file without downloading the rest of it.
type RangeFetcher interface {
	FetchRange(ctx context.Context, fileURL string, start, end int64) ([]byte, error)
}

// HTTPRangeFetcher fetches byte ranges over HTTP using the standard Range header.
type HTTPRangeFetcher struct {
	httpClient *http.Client
}

// NewHTTPRangeFetcher returns an HTTPRangeFetcher using hc, or
// http.DefaultClient if hc is nil.
func NewHTTPRangeFetcher(hc *http.Client) *HTTPRangeFetcher {
	if hc == nil {
		hc = http.DefaultClient
	}
	return &HTTPRangeFetcher{httpClient: hc}
}

// FetchRange implements RangeFetcher.
func (f *HTTPRangeFetcher) FetchRange(ctx context.Context, fileURL string, start, end int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("ingest: build range request: %w", err)
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch range %d-%d from %s: %w", start, end, fileURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ingest: unexpected status %d fetching range from %s", resp.StatusCode, fileURL)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ingest: read range response body: %w", err)
	}
	return data, nil
}

// safetensorsHeaderPrefixBytes is the fixed-size little-endian uint64
// prefix every safetensors file begins with, naming the byte length of
// the JSON header that immediately follows it.
const safetensorsHeaderPrefixBytes = 8

// ReadHeaderLength fetches only the first 8 bytes of a safetensors file,
// decodes the little-endian uint64 header length, and rejects it outright
// if it exceeds maxHeaderBytes (DefaultMaxHeaderBytes if <= 0) - before
// ever requesting the header content itself.
func ReadHeaderLength(ctx context.Context, fetcher RangeFetcher, fileURL string, maxHeaderBytes int64) (uint64, error) {
	if maxHeaderBytes <= 0 {
		maxHeaderBytes = DefaultMaxHeaderBytes
	}

	raw, err := fetcher.FetchRange(ctx, fileURL, 0, safetensorsHeaderPrefixBytes-1)
	if err != nil {
		return 0, fmt.Errorf("ingest: fetch header length prefix: %w", err)
	}
	if len(raw) != safetensorsHeaderPrefixBytes {
		return 0, fmt.Errorf("ingest: expected %d bytes for header length prefix, got %d", safetensorsHeaderPrefixBytes, len(raw))
	}

	length := binary.LittleEndian.Uint64(raw)
	if length > uint64(maxHeaderBytes) {
		return 0, fmt.Errorf("%w: header claims %d bytes, sanity bound is %d", ErrHeaderTooLarge, length, maxHeaderBytes)
	}

	return length, nil
}
