package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/minio/minio-go/v7"
	"golang.org/x/sync/errgroup"
)

// DefaultPartSize is 64MiB, comfortably above S3's 5MiB minimum part size
// and small enough that a single failed part costs little to retry.
const DefaultPartSize = 64 * 1024 * 1024

// DefaultUploadConcurrency bounds how many parts of one artifact upload
// concurrently.
const DefaultUploadConcurrency = 4

// UploadResult is the outcome of a completed chunked upload.
type UploadResult struct {
	ETag string
	Size int64
}

// ChunkedUploader uploads one artifact as a sequence of multipart chunks
// with bounded concurrency, tracking completed parts so an interrupted
// upload can resume without re-uploading data already accepted by the
// object store.
type ChunkedUploader struct {
	client      *Client
	partSize    int64
	concurrency int
}

// NewChunkedUploader returns a ChunkedUploader bound to client. partSize
// and concurrency fall back to DefaultPartSize and
// DefaultUploadConcurrency when <= 0.
func NewChunkedUploader(client *Client, partSize int64, concurrency int) *ChunkedUploader {
	if partSize <= 0 {
		partSize = DefaultPartSize
	}
	if concurrency <= 0 {
		concurrency = DefaultUploadConcurrency
	}
	return &ChunkedUploader{client: client, partSize: partSize, concurrency: concurrency}
}

// StartUpload initiates a new multipart upload and returns its upload ID.
// The caller should persist the ID somewhere durable (the ingestion job
// record) to make the upload resumable across a process restart.
func (u *ChunkedUploader) StartUpload(ctx context.Context, object string) (string, error) {
	uploadID, err := u.client.core.NewMultipartUpload(ctx, u.client.bucket, object, minio.PutObjectOptions{})
	if err != nil {
		return "", fmt.Errorf("cache: initiate multipart upload for %s: %w", object, err)
	}
	return uploadID, nil
}

// CompletedParts returns the parts an in-progress upload has already
// accepted, keyed by part number, so a resumed upload can skip
// re-uploading them.
func (u *ChunkedUploader) CompletedParts(ctx context.Context, object, uploadID string) (map[int]minio.CompletePart, error) {
	completed := map[int]minio.CompletePart{}

	marker := 0
	for {
		result, err := u.client.core.ListObjectParts(ctx, u.client.bucket, object, uploadID, marker, 1000)
		if err != nil {
			return nil, fmt.Errorf("cache: list completed parts for %s: %w", object, err)
		}
		for _, p := range result.ObjectParts {
			completed[p.PartNumber] = minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag}
		}
		if !result.IsTruncated {
			return completed, nil
		}
		marker = result.NextPartNumberMarker
	}
}

// AbortUpload cancels an in-progress multipart upload, releasing any
// parts already accepted by the object store.
func (u *ChunkedUploader) AbortUpload(ctx context.Context, object, uploadID string) error {
	if err := u.client.core.AbortMultipartUpload(ctx, u.client.bucket, object, uploadID); err != nil {
		return fmt.Errorf("cache: abort multipart upload for %s: %w", object, err)
	}
	return nil
}

// Upload streams src to object as a fresh multipart upload, chunking it
// into partSize pieces and uploading up to concurrency of them at once.
// It aborts the multipart upload on any failure so no partial upload
// lingers as unreclaimed storage.
func (u *ChunkedUploader) Upload(ctx context.Context, object string, src io.Reader) (*UploadResult, error) {
	uploadID, err := u.StartUpload(ctx, object)
	if err != nil {
		return nil, err
	}
	return u.Resume(ctx, object, uploadID, src, nil)
}

// Resume continues a multipart upload identified by uploadID, given the
// set of parts already completed (from CompletedParts, or nil to upload
// every part fresh). src is still read sequentially from the beginning:
// callers resuming a genuinely partial download are responsible for
// positioning src at the right offset first. Parts already present in
// alreadyCompleted are consumed from src to keep part numbering aligned,
// but are not re-uploaded.
func (u *ChunkedUploader) Resume(ctx context.Context, object, uploadID string, src io.Reader, alreadyCompleted map[int]minio.CompletePart) (*UploadResult, error) {
	succeeded := false
	defer func() {
		if !succeeded {
			_ = u.client.core.AbortMultipartUpload(context.Background(), u.client.bucket, object, uploadID)
		}
	}()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(u.concurrency)

	var (
		mu        sync.Mutex
		parts     []minio.CompletePart
		totalSize int64
	)

	buf := make([]byte, u.partSize)
	partNumber := 0

readLoop:
	for {
		n, readErr := io.ReadFull(src, buf)
		if n > 0 {
			partNumber++
			pn := partNumber
			totalSize += int64(n)

			if existing, ok := alreadyCompleted[pn]; ok {
				mu.Lock()
				parts = append(parts, existing)
				mu.Unlock()
			} else {
				data := make([]byte, n)
				copy(data, buf[:n])
				g.Go(func() error {
					objPart, err := u.client.core.PutObjectPart(gctx, u.client.bucket, object, uploadID, pn,
						bytes.NewReader(data), int64(len(data)), minio.PutObjectPartOptions{})
					if err != nil {
						return fmt.Errorf("cache: upload part %d of %s: %w", pn, object, err)
					}
					mu.Lock()
					parts = append(parts, minio.CompletePart{PartNumber: pn, ETag: objPart.ETag})
					mu.Unlock()
					return nil
				})
			}
		}

		switch {
		case readErr == nil:
			continue
		case errors.Is(readErr, io.EOF), errors.Is(readErr, io.ErrUnexpectedEOF):
			break readLoop
		default:
			_ = g.Wait()
			return nil, fmt.Errorf("cache: read artifact stream for %s: %w", object, readErr)
		}
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })

	info, err := u.client.core.CompleteMultipartUpload(ctx, u.client.bucket, object, uploadID, parts, minio.PutObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("cache: complete multipart upload for %s: %w", object, err)
	}

	succeeded = true
	return &UploadResult{ETag: info.ETag, Size: totalSize}, nil
}
