package telemetry_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/sanjayrohith/redline/internal/telemetry"
)

func TestHandler_StreamsPublishedSamplesToARealWebSocketClient(t *testing.T) {
	hub := telemetry.NewHub()
	server := httptest.NewServer(telemetry.Handler(hub))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = conn.CloseNow() }()

	// Give the server goroutine a moment to actually reach Subscribe()
	// before publishing, since Publish only fans out to subscribers
	// already registered at the time it's called.
	deadline := time.Now().Add(2 * time.Second)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1 before publishing", hub.SubscriberCount())
	}

	ttft := 42.5
	hub.Publish(telemetry.Sample{RunID: "run-abc", TTFTMs: &ttft, SampledAt: time.Now()})

	var got telemetry.Sample
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	if got.RunID != "run-abc" {
		t.Errorf("RunID = %q, want run-abc", got.RunID)
	}
	if got.TTFTMs == nil || *got.TTFTMs != 42.5 {
		t.Errorf("TTFTMs = %v, want 42.5", got.TTFTMs)
	}
}

func TestHandler_UnsubscribesOnClientDisconnect(t *testing.T) {
	hub := telemetry.NewHub()
	server := httptest.NewServer(telemetry.Handler(hub))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for hub.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1 while connected", hub.SubscriberCount())
	}

	_ = conn.Close(websocket.StatusNormalClosure, "done")

	deadline = time.Now().Add(2 * time.Second)
	for hub.SubscriberCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0 after client disconnect", hub.SubscriberCount())
	}
}
