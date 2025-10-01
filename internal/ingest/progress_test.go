package ingest

import (
	"bytes"
	"io"
	"testing"
)

func TestNewProgressReader_NilCallbackReturnsSourceUnwrapped(t *testing.T) {
	src := bytes.NewReader([]byte("hello"))
	if got := newProgressReader(src, nil); got != io.Reader(src) {
		t.Error("newProgressReader(src, nil) should return src unchanged")
	}
}

func TestProgressReader_ReportsCumulativeBytes(t *testing.T) {
	content := []byte("some bytes to read in small chunks")
	src := bytes.NewReader(content)

	var reports []int64
	r := newProgressReader(src, func(n int64) { reports = append(reports, n) })

	buf := make([]byte, 4)
	var total int
	for {
		n, err := r.Read(buf)
		total += n
		if err != nil {
			break
		}
	}

	if total != len(content) {
		t.Fatalf("total read = %d, want %d", total, len(content))
	}
	if len(reports) == 0 {
		t.Fatal("no progress reported")
	}
	if reports[len(reports)-1] != int64(len(content)) {
		t.Errorf("final report = %d, want %d", reports[len(reports)-1], len(content))
	}
}
