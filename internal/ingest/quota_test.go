package ingest

import (
	"errors"
	"testing"
)

func TestEnforceQuota_WithinLimits(t *testing.T) {
	files := []ManifestFile{{Path: "a", Size: 100}, {Path: "b", Size: 200}}
	if err := EnforceQuota(files, Quota{MaxTotalBytes: 1000, MaxFileCount: 5}); err != nil {
		t.Errorf("EnforceQuota() error = %v, want nil", err)
	}
}

func TestEnforceQuota_RejectsTotalSizeOverLimit(t *testing.T) {
	files := []ManifestFile{{Path: "a", Size: 600}, {Path: "b", Size: 600}}
	err := EnforceQuota(files, Quota{MaxTotalBytes: 1000, MaxFileCount: 5})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("error = %v, want ErrQuotaExceeded", err)
	}
}

func TestEnforceQuota_RejectsFileCountOverLimit(t *testing.T) {
	files := []ManifestFile{{Path: "a", Size: 1}, {Path: "b", Size: 1}, {Path: "c", Size: 1}}
	err := EnforceQuota(files, Quota{MaxTotalBytes: 1000, MaxFileCount: 2})
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("error = %v, want ErrQuotaExceeded", err)
	}
}

func TestEnforceQuota_ZeroFieldsFallBackToDefaults(t *testing.T) {
	files := []ManifestFile{{Path: "a", Size: 1024}}
	if err := EnforceQuota(files, Quota{}); err != nil {
		t.Errorf("EnforceQuota() error = %v, want nil under default quota", err)
	}
}

func TestEnforceQuota_ExactlyAtLimitIsAllowed(t *testing.T) {
	files := []ManifestFile{{Path: "a", Size: 500}, {Path: "b", Size: 500}}
	if err := EnforceQuota(files, Quota{MaxTotalBytes: 1000, MaxFileCount: 2}); err != nil {
		t.Errorf("EnforceQuota() error = %v, want nil at exactly the limit", err)
	}
}
