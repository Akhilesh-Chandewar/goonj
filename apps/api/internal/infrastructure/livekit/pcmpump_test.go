package livekit

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func mustCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func contextWithTimeout(t *testing.T, d time.Duration) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), d)
}

func TestPCMPumpStreamsFrames(t *testing.T) {
	// A fresh pump on a fixed high test port (Stop first so rebinds succeed).
	pump := NewPCMPump("127.0.0.1:19600", slog.Default())
	t.Cleanup(func() { pump.Stop(mustCtx(t)) })

	// Dial as an egress client would.
	dialCtx, cancel := contextWithTimeout(t, 2*time.Second)
	conn, _, err := websocket.Dial(dialCtx, "ws://127.0.0.1:19600/egress?trackID=TR_test", nil)
	cancel()
	if err != nil {
		t.Fatalf("dial pump: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "done") })

	select {
	case <-pump.Ready():
	case <-time.After(2 * time.Second):
		t.Fatal("pump never signaled ready")
	}

	// Send a binary PCM frame and a mute event.
	frame := []byte{1, 0, 2, 0, 3, 0}
	wctx, wcancel := contextWithTimeout(t, time.Second)
	if err := conn.Write(wctx, websocket.MessageBinary, frame); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	wcancel()
	wctx, wcancel = contextWithTimeout(t, time.Second)
	if err := conn.Write(wctx, websocket.MessageText, []byte(`{"muted":true}`)); err != nil {
		t.Fatalf("write mute: %v", err)
	}
	wcancel()

	select {
	case got := <-pump.Frames():
		if len(got) != len(frame) {
			t.Fatalf("frame len = %d, want %d", len(got), len(frame))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no frame received")
	}

	select {
	case m := <-pump.Muted():
		if !m {
			t.Fatal("mute event = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no mute event")
	}
}
