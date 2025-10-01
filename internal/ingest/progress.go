package ingest

import "io"

// ProgressFunc receives the cumulative byte count read so far. It is
// called synchronously on the download's hot path, so implementations
// used in production (e.g. persisting to a job record) should throttle
// or debounce their own writes rather than writing on every call.
type ProgressFunc func(bytesRead int64)

// progressReader wraps src, invoking onProgress with the running total
// after every read.
type progressReader struct {
	src        io.Reader
	total      int64
	onProgress ProgressFunc
}

func newProgressReader(src io.Reader, onProgress ProgressFunc) io.Reader {
	if onProgress == nil {
		return src
	}
	return &progressReader{src: src, onProgress: onProgress}
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.src.Read(buf)
	if n > 0 {
		p.total += int64(n)
		p.onProgress(p.total)
	}
	return n, err
}
