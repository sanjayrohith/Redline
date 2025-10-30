package telemetry

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// HeartbeatInterval is how often Handler pings an idle connection. A
// dead TCP connection stuck behind a NAT or firewall's connection table
// otherwise never gets cleaned up on the server side: the client is
// simply gone, but nothing tells the server that without an active probe.
const HeartbeatInterval = 30 * time.Second

// writeTimeout bounds how long a single sample write or ping may take
// before Handler gives up on the connection and closes it - a stalled
// write to one slow client must not hang the goroutine serving it
// forever.
const writeTimeout = 5 * time.Second

// Handler upgrades an HTTP request to a WebSocket connection and streams
// hub's telemetry samples to the client for the connection's lifetime.
// Backpressure is handled at the Hub level (see Hub.Publish); Handler's
// own job is delivering whatever it receives promptly, sending a
// periodic ping heartbeat, and closing cleanly on the first error,
// client disconnect, or request context cancellation.
func Handler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()

		// Accept hijacks the underlying connection, so it is no longer
		// tied to r.Context()'s usual client-disconnect cancellation
		// (net/http only tracks that for connections it still owns).
		// This is a push-only channel - the server never expects a
		// client message - but reading anyway is what actually detects
		// the client closing the connection: conn.Read returns promptly
		// once the peer sends a close frame or the TCP connection drops,
		// which is what lets Handler unsubscribe right away instead of
		// only noticing on the next failed heartbeat ping.
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			defer cancel()
			for {
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
			}
		}()

		samples, unsubscribe := hub.Subscribe()
		defer unsubscribe()

		heartbeat := time.NewTicker(HeartbeatInterval)
		defer heartbeat.Stop()

		for {
			select {
			case <-ctx.Done():
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return

			case sample, ok := <-samples:
				if !ok {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
				err := wsjson.Write(writeCtx, conn, sample)
				cancel()
				if err != nil {
					return
				}

			case <-heartbeat.C:
				pingCtx, cancel := context.WithTimeout(ctx, writeTimeout)
				err := conn.Ping(pingCtx)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}
}
