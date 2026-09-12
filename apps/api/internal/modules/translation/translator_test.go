package translation

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestDownsample48to24(t *testing.T) {
	// 8 samples at 48kHz → 4 samples at 24kHz (every other kept).
	var src []byte
	want := []int16{100, 300, 500, 700}
	for _, v := range []int16{100, 999, 300, 999, 500, 999, 700, 999} {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(v))
		src = append(src, b[:]...)
	}
	got := downsample48to24(src)
	if len(got) != len(want)*2 {
		t.Fatalf("len = %d bytes, want %d", len(got), len(want)*2)
	}
	for i, w := range want {
		g := int16(binary.LittleEndian.Uint16(got[i*2:]))
		if g != w {
			t.Fatalf("sample %d = %d, want %d", i, g, w)
		}
	}
	// Inputs too small for one output sample yield nothing (not garbage).
	if got := downsample48to24([]byte{1, 2}); len(got) != 0 {
		t.Fatalf("tiny input should yield 0 bytes, got %d", len(got))
	}
	// Odd trailing bytes are dropped safely (whole 48k samples only).
	if got := downsample48to24([]byte{1, 2, 3}); len(got) != 0 {
		t.Fatalf("odd input should yield 0 bytes, got %d", len(got))
	}
}

// publishCaption goes to the same channel shape the browser path uses —
// verify the envelope with a real Redis if one is up, else skip.
func TestPublishCaptionEnvelope(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:16380"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("no local redis; skipping pub/sub envelope test")
	}
	defer rdb.Close()

	sub := rdb.Subscribe(ctx, roomChannel("test-session"))
	defer sub.Close()

	tr := &Translator{rdb: rdb, log: slog.Default()}
	tr.publishCaption("test-session", "es", "hola mundo", true, 5, 900)

	select {
	case msg := <-sub.Channel():
		var env struct {
			Type    string          `json:"type"`
			Payload CaptionEnvelope `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Type != "caption" || env.Payload.Lang != "es" ||
			env.Payload.Text != "hola mundo" || !env.Payload.Final {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("caption never arrived on the room channel")
	}
}
