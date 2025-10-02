package cache

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sync/errgroup"
)

// DefaultPullConcurrency bounds how many files of one artifact are pulled
// from the cache concurrently onto local NVMe.
const DefaultPullConcurrency = 4

// ArtifactPuller pulls cached artifacts from the object store directly
// onto local disk, streaming rather than buffering, for the worker-node
// path that must have weights on NVMe before a container starts.
type ArtifactPuller struct {
	client *Client
}

// NewArtifactPuller returns an ArtifactPuller bound to client.
func NewArtifactPuller(client *Client) *ArtifactPuller {
	return &ArtifactPuller{client: client}
}

// PullToDisk streams objectKey from the cache to destPath, creating any
// missing parent directories. It removes a partially written file on any
// failure rather than leaving a truncated artifact on disk.
func (p *ArtifactPuller) PullToDisk(ctx context.Context, objectKey, destPath string) (int64, error) {
	obj, err := p.client.GetObject(ctx, objectKey)
	if err != nil {
		return 0, err
	}
	defer func() { _ = obj.Close() }()

	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return 0, fmt.Errorf("cache: create destination directory for %s: %w", destPath, err)
	}

	f, err := os.Create(destPath) // #nosec G304 -- destPath is operator/worker-controlled NVMe cache path, not request input
	if err != nil {
		return 0, fmt.Errorf("cache: create %s: %w", destPath, err)
	}

	n, copyErr := io.Copy(f, obj)
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("cache: pull %s to %s: %w", objectKey, destPath, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(destPath)
		return 0, fmt.Errorf("cache: close %s: %w", destPath, closeErr)
	}

	return n, nil
}

// PullSet pulls every objectKey->destPath pair in files, up to concurrency
// at once (DefaultPullConcurrency if <= 0), so a model's full artifact set
// lands on NVMe before its container is asked to start. It stops
// launching new pulls on the first failure and returns that error.
func (p *ArtifactPuller) PullSet(ctx context.Context, files map[string]string, concurrency int) error {
	if concurrency <= 0 {
		concurrency = DefaultPullConcurrency
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(concurrency)

	for objectKey, destPath := range files {
		g.Go(func() error {
			_, err := p.PullToDisk(gctx, objectKey, destPath)
			return err
		})
	}

	return g.Wait()
}
