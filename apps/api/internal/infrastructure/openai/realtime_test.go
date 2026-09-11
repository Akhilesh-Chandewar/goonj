package openai

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeOpenAI spins a WS server that records incoming events and can push
// scripted ones back. It stands in for wss://api.openai.com/v1/realtime.
type fakeOpenAI struct {
	srv      *httptest.Server
	url      string
	mu       sync.Mutex
	received []map[string]any
	gotCfg   chan struct{}
}

func newFakeOpenAI(t *testing.T) *fakeOpenAI {
	t.Helper()
	f := &fakeOpenAI{gotCfg: make(chan struct{}, 1)}
	upgrader := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "bye")
		for {
			_, raw, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var ev map[string]any
			if json.Unmarshal(raw, &ev) != nil {
				continue
			}
			f.mu.Lock()
			f.received = append(f.received, ev)
			f.mu.Unlock()
			if ev["type"] == "session.update" {
				f.gotCfg <- struct{}{}
			}
		}
	})
	f.srv = httptest.NewServer(upgrader)
	f.url = "ws" + f.srv.URL[len("http"):] // http:// → ws://
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenAI) events() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any{}, f.received...)
}

func (f *fakeOpenAI) push(t *testing.T, ev map[string]any) {
	t.Helper()
	// The client dials RealtimeBase; we can't inject the URL, so this helper
	// only exists for completeness. See TestAppendAudioWireFormat instead.
	_ = ev
}

func TestTranslationSessionSendAudioAfterConfig(t *testing.T) {
	fake := newFakeOpenAI(t)

	// Point the client at the fake server by overriding the base URL.
	oldBase := RealtimeBase
	RealtimeBase = fake.url
	t.Cleanup(func() { RealtimeBase = oldBase })

	sess, err := NewTranslationSession("test-key", "gpt-realtime-translate", "es",
		TranslationCallbacks{})
	if err != nil {
		t.Fatalf("dial fake server: %v", err)
	}
	defer sess.CloseNow()

	select {
	case <-fake.gotCfg:
	case <-time.After(2 * time.Second):
		t.Fatal("session.update not sent before audio")
	}

	pcm := make([]byte, 1920) // 40ms @24kHz stereo-equivalent framing
	if err := sess.AppendAudio(pcm); err != nil {
		t.Fatalf("append audio: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		evs := fake.events()
		if len(evs) >= 2 {
			// First event must be the config, second the audio append.
			if evs[0]["type"] != "session.update" {
				t.Fatalf("first event = %v, want session.update", evs[0]["type"])
			}
			if evs[1]["type"] != "session.input_audio_buffer.append" {
				t.Fatalf("second event = %v, want audio append", evs[1]["type"])
			}
			audio, _ := evs[1]["audio"].(string)
			if got, err := base64.StdEncoding.DecodeString(audio); err != nil || len(got) != len(pcm) {
				t.Fatalf("audio payload mismatch: %d bytes, err=%v", len(got), err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected config + audio events, got %d", len(fake.events()))
}
