package cache

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestChunkedUploader_UploadRoundTrip(t *testing.T) {
	client := newTestClient(t, "test-uploads")
	uploader := NewChunkedUploader(client, 5*1024*1024, 3) // 5MiB parts, min allowed by S3

	content := make([]byte, 12*1024*1024) // spans 3 parts: 5 + 5 + 2 MiB
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("generate random content: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := uploader.Upload(ctx, "artifacts/model.safetensors", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
	if result.ETag == "" {
		t.Error("ETag is empty")
	}

	obj, err := client.minio.GetObject(ctx, client.bucket, "artifacts/model.safetensors", minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	defer func() { _ = obj.Close() }()

	got, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read uploaded object: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded object content does not match what was uploaded")
	}
}

func TestChunkedUploader_UploadSinglePartSmallerThanPartSize(t *testing.T) {
	client := newTestClient(t, "test-uploads-small")
	uploader := NewChunkedUploader(client, 5*1024*1024, 2)

	content := []byte("small artifact, one part")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := uploader.Upload(ctx, "artifacts/small.bin", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
}

func TestChunkedUploader_AbortsOnReadError(t *testing.T) {
	client := newTestClient(t, "test-uploads-abort")
	uploader := NewChunkedUploader(client, 5*1024*1024, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	failing := &failingReader{failAfter: 6 * 1024 * 1024} // fail partway into part 2
	_, err := uploader.Upload(ctx, "artifacts/aborted.bin", failing)
	if err == nil {
		t.Fatal("Upload() error = nil, want error from the failing reader")
	}

	// The multipart upload should have been aborted: no parts should
	// remain listed against a fresh upload of the same object.
	uploads, err := client.core.ListMultipartUploads(ctx, client.bucket, "artifacts/aborted.bin", "", "", "", 10)
	if err != nil {
		t.Fatalf("ListMultipartUploads() error = %v", err)
	}
	if len(uploads.Uploads) != 0 {
		t.Errorf("len(Uploads) = %d, want 0 (aborted upload should not linger)", len(uploads.Uploads))
	}
}

func TestChunkedUploader_ResumeSkipsCompletedParts(t *testing.T) {
	client := newTestClient(t, "test-uploads-resume")
	uploader := NewChunkedUploader(client, 5*1024*1024, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	object := "artifacts/resumed.bin"
	content := make([]byte, 11*1024*1024) // 2 parts: 5 + 5, plus a short 3rd... actually 11MiB = 5+5+1
	if _, err := rand.Read(content); err != nil {
		t.Fatalf("generate random content: %v", err)
	}

	uploadID, err := uploader.StartUpload(ctx, object)
	if err != nil {
		t.Fatalf("StartUpload() error = %v", err)
	}

	// Manually upload only the first part directly, simulating a process
	// that crashed after part 1 but before completing the upload.
	firstPart := content[:5*1024*1024]
	part1, err := client.core.PutObjectPart(ctx, client.bucket, object, uploadID, 1, bytes.NewReader(firstPart), int64(len(firstPart)), minio.PutObjectPartOptions{})
	if err != nil {
		t.Fatalf("PutObjectPart() error = %v", err)
	}

	completed, err := uploader.CompletedParts(ctx, object, uploadID)
	if err != nil {
		t.Fatalf("CompletedParts() error = %v", err)
	}
	if len(completed) != 1 || completed[1].ETag == "" {
		t.Fatalf("CompletedParts() = %+v, want exactly one part with a non-empty ETag (server-reported ETag %q)", completed, part1.ETag)
	}

	result, err := uploader.Resume(ctx, object, uploadID, bytes.NewReader(content), completed)
	if err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}

	obj, err := client.minio.GetObject(ctx, client.bucket, object, minio.GetObjectOptions{})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	defer func() { _ = obj.Close() }()

	got, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read resumed object: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("resumed object content does not match the original")
	}
}

type failingReader struct {
	read      int
	failAfter int
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.read >= f.failAfter {
		return 0, io.ErrClosedPipe
	}
	remaining := f.failAfter - f.read
	if len(p) > remaining {
		p = p[:remaining]
	}
	for i := range p {
		p[i] = 'x'
	}
	f.read += len(p)
	return len(p), nil
}
