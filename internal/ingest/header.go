package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// TensorInfo is one tensor's metadata from a safetensors header: its name,
// dtype, shape, and byte offsets into the payload region this package
// never fetches.
type TensorInfo struct {
	Name        string
	Dtype       string
	Shape       []int64
	DataOffsets [2]int64
}

// Header is a safetensors file's fully decoded, typed header: every
// tensor's metadata plus any free-form "__metadata__" block, with no
// tensor payload byte ever read.
type Header struct {
	Tensors  []TensorInfo
	Metadata map[string]string
}

type tensorEntry struct {
	Dtype       string   `json:"dtype"`
	Shape       []int64  `json:"shape"`
	DataOffsets [2]int64 `json:"data_offsets"`
}

const metadataKey = "__metadata__"

// HeaderLimits bounds header parsing so a crafted file cannot drive
// unbounded allocation: DefaultMaxHeaderBytes and DefaultMaxTensorCount
// are both far beyond any real model, with room to spare.
type HeaderLimits struct {
	MaxHeaderBytes int64
	MaxTensorCount int
}

// DefaultMaxTensorCount bounds the number of tensor entries a header may
// declare. The largest real-world models have on the order of a few
// thousand tensors; this leaves two orders of magnitude of headroom.
const DefaultMaxTensorCount = 200_000

// maxShapeRank bounds a single tensor's dimensionality. No real model
// architecture exceeds a handful of dimensions per tensor.
const maxShapeRank = 16

// validDtypes is the safetensors spec's fixed set of dtype tokens. A
// header claiming anything else is malformed or hostile.
var validDtypes = map[string]bool{
	"BOOL": true, "U8": true, "I8": true, "U16": true, "I16": true,
	"U32": true, "I32": true, "U64": true, "I64": true,
	"F16": true, "BF16": true, "F32": true, "F64": true,
	"F8_E4M3": true, "F8_E5M2": true,
}

func (l HeaderLimits) normalize() HeaderLimits {
	if l.MaxHeaderBytes <= 0 {
		l.MaxHeaderBytes = DefaultMaxHeaderBytes
	}
	if l.MaxTensorCount <= 0 {
		l.MaxTensorCount = DefaultMaxTensorCount
	}
	return l
}

// FetchHeader reads a safetensors file's length-prefixed JSON header -
// first the 8-byte length prefix, then exactly that many header bytes -
// and decodes it into a typed Header, enforcing limits throughout. It
// never requests any byte of the tensor payload region that follows.
func FetchHeader(ctx context.Context, fetcher RangeFetcher, fileURL string, limits HeaderLimits) (*Header, error) {
	limits = limits.normalize()

	headerLength, err := ReadHeaderLength(ctx, fetcher, fileURL, limits.MaxHeaderBytes)
	if err != nil {
		return nil, err
	}

	start := int64(safetensorsHeaderPrefixBytes)
	end := start + int64(headerLength) - 1 // #nosec G115 -- ReadHeaderLength already bounded headerLength to limits.MaxHeaderBytes

	raw, err := fetcher.FetchRange(ctx, fileURL, start, end)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch header content: %w", err)
	}
	if uint64(len(raw)) != headerLength {
		return nil, fmt.Errorf("ingest: expected %d header bytes, got %d", headerLength, len(raw))
	}

	return decodeHeader(raw, limits)
}

// ErrTooManyTensors marks a header declaring more tensors than limits allow.
var ErrTooManyTensors = errors.New("ingest: safetensors header declares too many tensors")

// ErrInvalidTensorMetadata marks a header whose tensor entry fails
// structural validation: an unknown dtype, a negative or excessive-rank
// shape, or a malformed data_offsets pair.
var ErrInvalidTensorMetadata = errors.New("ingest: invalid tensor metadata")

func decodeHeader(raw []byte, limits HeaderLimits) (*Header, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("ingest: decode safetensors header json: %w", err)
	}

	// __metadata__ is not a tensor, so it does not count against the
	// tensor limit; every other key does.
	tensorCount := len(entries)
	if _, hasMetadata := entries[metadataKey]; hasMetadata {
		tensorCount--
	}
	if tensorCount > limits.MaxTensorCount {
		return nil, fmt.Errorf("%w: %d tensors exceeds the limit of %d", ErrTooManyTensors, tensorCount, limits.MaxTensorCount)
	}

	header := &Header{}
	for name, rawEntry := range entries {
		if name == metadataKey {
			var meta map[string]string
			if err := json.Unmarshal(rawEntry, &meta); err != nil {
				return nil, fmt.Errorf("ingest: decode %s: %w", metadataKey, err)
			}
			header.Metadata = meta
			continue
		}

		var t tensorEntry
		if err := json.Unmarshal(rawEntry, &t); err != nil {
			return nil, fmt.Errorf("ingest: decode tensor %q: %w", name, err)
		}
		if err := validateTensorEntry(name, t); err != nil {
			return nil, err
		}

		header.Tensors = append(header.Tensors, TensorInfo{
			Name: name, Dtype: t.Dtype, Shape: t.Shape, DataOffsets: t.DataOffsets,
		})
	}

	sort.Slice(header.Tensors, func(i, j int) bool { return header.Tensors[i].Name < header.Tensors[j].Name })

	return header, nil
}

func validateTensorEntry(name string, t tensorEntry) error {
	if !validDtypes[t.Dtype] {
		return fmt.Errorf("%w: tensor %q has unknown dtype %q", ErrInvalidTensorMetadata, name, t.Dtype)
	}
	if len(t.Shape) > maxShapeRank {
		return fmt.Errorf("%w: tensor %q has rank %d, exceeding the limit of %d", ErrInvalidTensorMetadata, name, len(t.Shape), maxShapeRank)
	}
	for _, dim := range t.Shape {
		if dim < 0 {
			return fmt.Errorf("%w: tensor %q has a negative shape dimension %d", ErrInvalidTensorMetadata, name, dim)
		}
	}
	if t.DataOffsets[0] < 0 || t.DataOffsets[1] < t.DataOffsets[0] {
		return fmt.Errorf("%w: tensor %q has invalid data_offsets %v", ErrInvalidTensorMetadata, name, t.DataOffsets)
	}
	return nil
}
