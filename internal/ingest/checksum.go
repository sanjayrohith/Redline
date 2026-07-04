package ingest

import (
	"context"
	"crypto/sha1" //nolint:gosec // sha1 is git's blob-oid algorithm, used only for content-identity matching, not security
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"strings"
)

// ChecksumMismatchError reports that a downloaded artifact's computed
// digest did not match the digest the upstream repository published for it.
type ChecksumMismatchError struct {
	Path string
	Want string
	Got  string
	// QuarantineKey is where the mismatched object was moved for
	// inspection, set only by CachePipeline.DownloadToCache - other
	// producers of this error (e.g. DownloadAndVerify's local-disk path)
	// leave it empty.
	QuarantineKey string
}

func (e *ChecksumMismatchError) Error() string {
	return fmt.Sprintf("ingest: checksum mismatch for %s: want %s, got %s", e.Path, e.Want, e.Got)
}

func newHasher(algorithm string) (hash.Hash, error) {
	switch strings.ToLower(algorithm) {
	case "sha256":
		return sha256.New(), nil
	case "sha1":
		return sha1.New(), nil //nolint:gosec // see import comment
	default:
		return nil, fmt.Errorf("ingest: unsupported checksum algorithm %q", algorithm)
	}
}

// StreamAndVerify copies src to dst while hashing every byte with
// algorithm, comparing the final digest against expectedHexDigest once the
// stream is exhausted. dst has already received every byte by the time an
// error is returned, so the caller is responsible for discarding it (e.g.
// removing the file it was writing to) on any non-nil error.
func StreamAndVerify(dst io.Writer, src io.Reader, algorithm, expectedHexDigest string, path string) error {
	h, err := newHasher(algorithm)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, io.TeeReader(src, h)); err != nil {
		return fmt.Errorf("ingest: stream artifact %s: %w", path, err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, expectedHexDigest) {
		return &ChecksumMismatchError{Path: path, Want: expectedHexDigest, Got: got}
	}

	return nil
}

// DownloadAndVerify streams fileURL to destPath, verifying its content
// against algorithm/expectedHexDigest as it downloads, and deletes
// destPath if the checksum does not match rather than leaving a
// corrupted or tampered artifact on disk.
func DownloadAndVerify(ctx context.Context, httpClient *http.Client, fileURL, algorithm, expectedHexDigest, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("ingest: build download request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ingest: download %s: %w", fileURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ingest: unexpected status %d downloading %s", resp.StatusCode, fileURL)
	}

	f, err := os.Create(destPath) // #nosec G304 -- destPath is operator-controlled cache path, not request input
	if err != nil {
		return fmt.Errorf("ingest: create %s: %w", destPath, err)
	}

	verifyErr := StreamAndVerify(f, resp.Body, algorithm, expectedHexDigest, fileURL)
	closeErr := f.Close()

	if verifyErr != nil {
		_ = os.Remove(destPath)
		return verifyErr
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("ingest: close %s: %w", destPath, closeErr)
	}

	return nil
}
