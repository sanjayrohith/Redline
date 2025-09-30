package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestMalformedSafetensorsFixtures drives FetchHeader over a fixed set of
// hostile or corrupted safetensors files checked into testdata/malformed,
// asserting every one is rejected cleanly - no panic, always an error -
// regardless of which layer catches it (the length-prefix sanity bound,
// JSON decoding, or per-tensor validation).
func TestMalformedSafetensorsFixtures(t *testing.T) {
	fixtures := []string{
		"truncated_header.safetensors",
		"oversized_length_prefix.safetensors",
		"invalid_json_header.safetensors",
		"absurd_tensor_shape.safetensors",
		"too_many_tensors.safetensors",
		"negative_shape_dimension.safetensors",
		"unknown_dtype.safetensors",
		"short_length_prefix.safetensors",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("testdata", "malformed", name)
			data, err := os.ReadFile(path) // #nosec G304 -- fixed testdata path, not request input
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}

			fetcher := &fakeRangeFetcher{data: data}

			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("FetchHeader panicked on fixture %s: %v", name, r)
				}
			}()

			_, err = FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{
				MaxTensorCount: 1000, // deliberately tighter than too_many_tensors.safetensors's 5000 entries
			})
			if err == nil {
				t.Errorf("FetchHeader(%s) error = nil, want a rejection", name)
			}
		})
	}
}

func TestMalformedSafetensorsFixtures_AllFilesAccountedFor(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "malformed"))
	if err != nil {
		t.Fatalf("read testdata/malformed: %v", err)
	}
	if len(entries) != 8 {
		t.Errorf("testdata/malformed has %d files, want 8 - update this test and TestMalformedSafetensorsFixtures together if fixtures changed", len(entries))
	}
}
