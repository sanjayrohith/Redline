package ingest

import (
	"errors"
	"fmt"
)

// ErrQuotaExceeded marks a repository whose manifest exceeds the
// configured ingestion quota, rejected before any file is downloaded.
var ErrQuotaExceeded = errors.New("ingest: repository exceeds ingestion quota")

// Quota bounds one ingestion: the total byte size across every file, and
// the number of files, so a single repository cannot exhaust a worker
// node's NVMe capacity or overwhelm its filesystem with tiny files.
type Quota struct {
	MaxTotalBytes int64
	MaxFileCount  int
}

// DefaultQuota is a generous ceiling: 500 GiB total and 10,000 files,
// well beyond any real single-model repository, but bounded nonetheless.
var DefaultQuota = Quota{
	MaxTotalBytes: 500 * 1024 * 1024 * 1024,
	MaxFileCount:  10_000,
}

func (q Quota) normalize() Quota {
	if q.MaxTotalBytes <= 0 {
		q.MaxTotalBytes = DefaultQuota.MaxTotalBytes
	}
	if q.MaxFileCount <= 0 {
		q.MaxFileCount = DefaultQuota.MaxFileCount
	}
	return q
}

// EnforceQuota rejects files if it exceeds quota's file count or total
// byte ceiling.
func EnforceQuota(files []ManifestFile, quota Quota) error {
	quota = quota.normalize()

	if len(files) > quota.MaxFileCount {
		return fmt.Errorf("%w: %d files exceeds the limit of %d", ErrQuotaExceeded, len(files), quota.MaxFileCount)
	}

	var total int64
	for _, f := range files {
		total += f.Size
	}
	if total > quota.MaxTotalBytes {
		return fmt.Errorf("%w: total size %d bytes exceeds the limit of %d bytes", ErrQuotaExceeded, total, quota.MaxTotalBytes)
	}

	return nil
}
