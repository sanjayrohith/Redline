package ingest

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeRangeFetcher struct {
	data []byte
	err  error
}

func (f *fakeRangeFetcher) FetchRange(_ context.Context, _ string, start, end int64) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if end >= int64(len(f.data)) {
		end = int64(len(f.data)) - 1
	}
	if start > end {
		return nil, nil
	}
	return f.data[start : end+1], nil
}

func lengthPrefix(n uint64) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, n)
	return buf
}

func TestReadHeaderLength_Valid(t *testing.T) {
	fetcher := &fakeRangeFetcher{data: lengthPrefix(1024)}

	length, err := ReadHeaderLength(context.Background(), fetcher, "https://example.com/model.safetensors", 0)
	if err != nil {
		t.Fatalf("ReadHeaderLength() error = %v", err)
	}
	if length != 1024 {
		t.Errorf("length = %d, want 1024", length)
	}
}

func TestReadHeaderLength_RejectsOversizedLength(t *testing.T) {
	fetcher := &fakeRangeFetcher{data: lengthPrefix(1 << 40)} // absurd: 1TB claimed header

	_, err := ReadHeaderLength(context.Background(), fetcher, "https://example.com/model.safetensors", DefaultMaxHeaderBytes)
	if !errors.Is(err, ErrHeaderTooLarge) {
		t.Errorf("error = %v, want ErrHeaderTooLarge", err)
	}
}

func TestReadHeaderLength_RespectsCustomBound(t *testing.T) {
	fetcher := &fakeRangeFetcher{data: lengthPrefix(2048)}

	_, err := ReadHeaderLength(context.Background(), fetcher, "https://example.com/model.safetensors", 1024)
	if !errors.Is(err, ErrHeaderTooLarge) {
		t.Errorf("error = %v, want ErrHeaderTooLarge with a custom 1024-byte bound", err)
	}
}

func TestReadHeaderLength_ShortRead(t *testing.T) {
	fetcher := &fakeRangeFetcher{data: []byte{1, 2, 3}}

	if _, err := ReadHeaderLength(context.Background(), fetcher, "https://example.com/model.safetensors", 0); err == nil {
		t.Fatal("ReadHeaderLength() error = nil, want error for short read")
	}
}

func TestReadHeaderLength_FetchError(t *testing.T) {
	fetcher := &fakeRangeFetcher{err: errors.New("connection reset")}

	if _, err := ReadHeaderLength(context.Background(), fetcher, "https://example.com/model.safetensors", 0); err == nil {
		t.Fatal("ReadHeaderLength() error = nil, want error propagated from fetcher")
	}
}

func TestHTTPRangeFetcher_SendsRangeHeaderAndReturnsBody(t *testing.T) {
	var gotRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(lengthPrefix(42))
	}))
	defer server.Close()

	fetcher := NewHTTPRangeFetcher(server.Client())
	data, err := fetcher.FetchRange(context.Background(), server.URL, 0, 7)
	if err != nil {
		t.Fatalf("FetchRange() error = %v", err)
	}

	if gotRange != "bytes=0-7" {
		t.Errorf("Range header = %q, want bytes=0-7", gotRange)
	}
	if binary.LittleEndian.Uint64(data) != 42 {
		t.Errorf("decoded length = %d, want 42", binary.LittleEndian.Uint64(data))
	}
}

func TestHTTPRangeFetcher_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	fetcher := NewHTTPRangeFetcher(server.Client())
	if _, err := fetcher.FetchRange(context.Background(), server.URL, 0, 7); err == nil {
		t.Fatal("FetchRange() error = nil, want error for 404")
	}
}
