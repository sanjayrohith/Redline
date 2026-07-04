package ingest

import (
	"context"
	"errors"
	"time"

	"github.com/sanjayrohith/redline/internal/cache"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

// DefaultDownloadRetryPolicy backs off the same way scheduler's placement
// retries do: capped exponential with full jitter, reused here rather
// than reimplemented, since "how long to wait before trying again" is the
// same question regardless of what failed.
var DefaultDownloadRetryPolicy = scheduler.DefaultRetryPolicy

// RetryingDownload calls pipeline.DownloadToCache, retrying on a checksum
// mismatch - the artifact quarantined, and re-fetched from source, up to
// policy.MaxAttempts times with exponential backoff between attempts.
// Any other error (a network failure, an upload failure) is returned
// immediately without retry: a checksum mismatch is the one failure class
// this function knows is worth trying again for, since it plausibly means
// a corrupted transfer rather than a permanently broken reference.
func RetryingDownload(ctx context.Context, pipeline *CachePipeline, policy scheduler.RetryPolicy, fileURL, algorithm, expectedHexDigest, objectKey string, onProgress ProgressFunc) (*cache.UploadResult, error) {
	if policy.MaxAttempts <= 0 {
		policy = DefaultDownloadRetryPolicy
	}

	var lastErr error
	for attempt := 1; ; attempt++ {
		result, err := pipeline.DownloadToCache(ctx, fileURL, algorithm, expectedHexDigest, objectKey, onProgress)
		if err == nil {
			return result, nil
		}

		var mismatch *ChecksumMismatchError
		if !errors.As(err, &mismatch) {
			return nil, err
		}
		lastErr = err

		if attempt >= policy.MaxAttempts {
			return nil, lastErr
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(policy.NextDelay(attempt)):
		}
	}
}
