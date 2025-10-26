package scheduler

import "testing"

func TestReducedFootprintBytes(t *testing.T) {
	got := ReducedFootprintBytes(100_000_000_000)
	want := int64(90_000_000_000)
	if got != want {
		t.Errorf("ReducedFootprintBytes(100e9) = %d, want %d", got, want)
	}
}

func TestReducedFootprintBytes_IsStrictlySmaller(t *testing.T) {
	original := int64(48 << 30)
	reduced := ReducedFootprintBytes(original)
	if reduced >= original {
		t.Errorf("ReducedFootprintBytes(%d) = %d, want strictly less than the original", original, reduced)
	}
}
