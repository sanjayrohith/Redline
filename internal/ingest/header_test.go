package ingest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func fakeSafetensorsFile(headerJSON string, payloadBytes int) []byte {
	header := []byte(headerJSON)
	prefix := lengthPrefix(uint64(len(header)))
	payload := make([]byte, payloadBytes)
	return append(append(prefix, header...), payload...)
}

func TestFetchHeader_DecodesTensorsAndMetadata(t *testing.T) {
	headerJSON := `{
		"__metadata__": {"format": "pt"},
		"weight": {"dtype": "F16", "shape": [4096, 4096], "data_offsets": [0, 33554432]},
		"bias": {"dtype": "F16", "shape": [4096], "data_offsets": [33554432, 33562624]}
	}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 1024)}

	header, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{})
	if err != nil {
		t.Fatalf("FetchHeader() error = %v", err)
	}

	if header.Metadata["format"] != "pt" {
		t.Errorf("Metadata[format] = %q, want pt", header.Metadata["format"])
	}
	if len(header.Tensors) != 2 {
		t.Fatalf("len(Tensors) = %d, want 2", len(header.Tensors))
	}
	// Sorted by name: "bias" before "weight".
	if header.Tensors[0].Name != "bias" || header.Tensors[1].Name != "weight" {
		t.Errorf("tensor order = [%s, %s], want [bias, weight]", header.Tensors[0].Name, header.Tensors[1].Name)
	}
	weight := header.Tensors[1]
	if weight.Dtype != "F16" {
		t.Errorf("weight.Dtype = %q, want F16", weight.Dtype)
	}
	if len(weight.Shape) != 2 || weight.Shape[0] != 4096 || weight.Shape[1] != 4096 {
		t.Errorf("weight.Shape = %v, want [4096 4096]", weight.Shape)
	}
	if weight.DataOffsets != [2]int64{0, 33554432} {
		t.Errorf("weight.DataOffsets = %v, want [0 33554432]", weight.DataOffsets)
	}
}

func TestFetchHeader_NoMetadataBlock(t *testing.T) {
	headerJSON := `{"weight": {"dtype": "F32", "shape": [10], "data_offsets": [0, 40]}}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 0)}

	header, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{})
	if err != nil {
		t.Fatalf("FetchHeader() error = %v", err)
	}
	if header.Metadata != nil {
		t.Errorf("Metadata = %v, want nil", header.Metadata)
	}
	if len(header.Tensors) != 1 {
		t.Fatalf("len(Tensors) = %d, want 1", len(header.Tensors))
	}
}

func TestFetchHeader_NeverFetchesPastTheHeader(t *testing.T) {
	headerJSON := `{"weight": {"dtype": "F32", "shape": [10], "data_offsets": [0, 40]}}`
	full := fakeSafetensorsFile(headerJSON, 1_000_000) // large simulated payload
	fetcher := &boundedRangeFetcher{data: full, maxEnd: int64(8 + len(headerJSON) - 1)}

	if _, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{}); err != nil {
		t.Fatalf("FetchHeader() error = %v", err)
	}
}

// boundedRangeFetcher fails any request reaching past maxEnd, proving the
// header parser never requests a byte of the tensor payload region.
type boundedRangeFetcher struct {
	data   []byte
	maxEnd int64
}

func (f *boundedRangeFetcher) FetchRange(_ context.Context, _ string, start, end int64) ([]byte, error) {
	if end > f.maxEnd {
		return nil, errBoundedFetchPastHeader
	}
	return f.data[start : end+1], nil
}

var errBoundedFetchPastHeader = &boundedFetchError{}

type boundedFetchError struct{}

func (*boundedFetchError) Error() string {
	return "ingest: test fetcher refused a read past the header"
}

func TestFetchHeader_MalformedJSON(t *testing.T) {
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(`not json`, 0)}

	if _, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{}); err == nil {
		t.Fatal("FetchHeader() error = nil, want error for malformed JSON")
	}
}

func TestFetchHeader_TruncatedHeaderBytes(t *testing.T) {
	headerJSON := `{"weight": {"dtype": "F32", "shape": [10], "data_offsets": [0, 40]}}`
	full := fakeSafetensorsFile(headerJSON, 0)
	truncated := full[:len(full)-5] // lose the last 5 header bytes
	fetcher := &fakeRangeFetcher{data: truncated}

	if _, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{}); err == nil {
		t.Fatal("FetchHeader() error = nil, want error for truncated header")
	}
}

func TestFetchHeader_RejectsTooManyTensors(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("{")
	for i := 0; i < 5; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"t%d": {"dtype": "F32", "shape": [1], "data_offsets": [0, 4]}`, i)
	}
	sb.WriteString("}")

	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(sb.String(), 0)}

	_, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{MaxTensorCount: 3})
	if !errors.Is(err, ErrTooManyTensors) {
		t.Errorf("error = %v, want ErrTooManyTensors", err)
	}
}

func TestFetchHeader_MetadataDoesNotCountTowardTensorLimit(t *testing.T) {
	headerJSON := `{
		"__metadata__": {"format": "pt"},
		"weight": {"dtype": "F32", "shape": [1], "data_offsets": [0, 4]}
	}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 0)}

	_, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{MaxTensorCount: 1})
	if err != nil {
		t.Errorf("FetchHeader() error = %v, want nil (metadata block should not count as a tensor)", err)
	}
}

func TestFetchHeader_RejectsUnknownDtype(t *testing.T) {
	headerJSON := `{"weight": {"dtype": "MYSTERY", "shape": [1], "data_offsets": [0, 4]}}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 0)}

	_, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{})
	if !errors.Is(err, ErrInvalidTensorMetadata) {
		t.Errorf("error = %v, want ErrInvalidTensorMetadata", err)
	}
}

func TestFetchHeader_RejectsNegativeShapeDimension(t *testing.T) {
	headerJSON := `{"weight": {"dtype": "F32", "shape": [-1, 10], "data_offsets": [0, 40]}}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 0)}

	_, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{})
	if !errors.Is(err, ErrInvalidTensorMetadata) {
		t.Errorf("error = %v, want ErrInvalidTensorMetadata", err)
	}
}

func TestFetchHeader_RejectsExcessiveRank(t *testing.T) {
	shape := make([]string, maxShapeRank+1)
	for i := range shape {
		shape[i] = "1"
	}
	headerJSON := fmt.Sprintf(`{"weight": {"dtype": "F32", "shape": [%s], "data_offsets": [0, 4]}}`, strings.Join(shape, ","))
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 0)}

	_, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{})
	if !errors.Is(err, ErrInvalidTensorMetadata) {
		t.Errorf("error = %v, want ErrInvalidTensorMetadata", err)
	}
}

func TestFetchHeader_RejectsInvertedDataOffsets(t *testing.T) {
	headerJSON := `{"weight": {"dtype": "F32", "shape": [10], "data_offsets": [40, 0]}}`
	fetcher := &fakeRangeFetcher{data: fakeSafetensorsFile(headerJSON, 0)}

	_, err := FetchHeader(context.Background(), fetcher, "https://example.com/model.safetensors", HeaderLimits{})
	if !errors.Is(err, ErrInvalidTensorMetadata) {
		t.Errorf("error = %v, want ErrInvalidTensorMetadata", err)
	}
}
