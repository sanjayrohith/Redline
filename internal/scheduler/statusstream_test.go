package scheduler

import "testing"

func TestDeploymentIDFromJobID(t *testing.T) {
	tests := []struct {
		jobID  string
		wantID string
		wantOK bool
	}{
		{"inference-dep-123", "dep-123", true},
		{"inference-", "", false},
		{"other-job", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		gotID, gotOK := deploymentIDFromJobID(tt.jobID)
		if gotID != tt.wantID || gotOK != tt.wantOK {
			t.Errorf("deploymentIDFromJobID(%q) = (%q, %v), want (%q, %v)", tt.jobID, gotID, gotOK, tt.wantID, tt.wantOK)
		}
	}
}

func TestMapAllocationState(t *testing.T) {
	tests := map[string]string{
		"pending":  "provisioning",
		"running":  "ready",
		"complete": "terminated",
		"failed":   "failed",
		"lost":     "failed",
		"unknown":  "",
	}
	for status, want := range tests {
		if got := mapAllocationState(status); got != want {
			t.Errorf("mapAllocationState(%q) = %q, want %q", status, got, want)
		}
	}
}
