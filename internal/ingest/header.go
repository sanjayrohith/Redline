package ingest

import (
	"context"
	"encoding/json"
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

// FetchHeader reads a safetensors file's length-prefixed JSON header -
// first the 8-byte length prefix, then exactly that many header bytes -
// and decodes it into a typed Header. It never requests any byte of the
// tensor payload region that follows.
func FetchHeader(ctx context.Context, fetcher RangeFetcher, fileURL string, maxHeaderBytes int64) (*Header, error) {
	headerLength, err := ReadHeaderLength(ctx, fetcher, fileURL, maxHeaderBytes)
	if err != nil {
		return nil, err
	}

	start := int64(safetensorsHeaderPrefixBytes)
	end := start + int64(headerLength) - 1 // #nosec G115 -- ReadHeaderLength already bounded headerLength to maxHeaderBytes

	raw, err := fetcher.FetchRange(ctx, fileURL, start, end)
	if err != nil {
		return nil, fmt.Errorf("ingest: fetch header content: %w", err)
	}
	if uint64(len(raw)) != headerLength {
		return nil, fmt.Errorf("ingest: expected %d header bytes, got %d", headerLength, len(raw))
	}

	return decodeHeader(raw)
}

func decodeHeader(raw []byte) (*Header, error) {
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("ingest: decode safetensors header json: %w", err)
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
		header.Tensors = append(header.Tensors, TensorInfo{
			Name: name, Dtype: t.Dtype, Shape: t.Shape, DataOffsets: t.DataOffsets,
		})
	}

	sort.Slice(header.Tensors, func(i, j int) bool { return header.Tensors[i].Name < header.Tensors[j].Name })

	return header, nil
}
