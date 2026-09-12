package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Translated-audio streaming: a second WebSocket per listener that carries
// the OpenAI-translated speech as raw PCM16 frames (24kHz mono) — enough for
// the browser to buffer and play through a WebAudio Worklet-free pipeline
// (ScriptProcessor-free: we use AudioContext + BufferSource chaining).
//
// Why a separate socket instead of the chat channel: audio frames are binary
// and high-rate; mixing them into the JSON text channel would force base64
// overhead (~33%) on every frame and risk starving chat. The gateway's own
// comment ("raw audio never flows through WebSockets") still holds for the
// SOURCE audio — listeners get the creator's voice over WebRTC from LiveKit;
// only the small, per-language translated stream rides here.

// translateAudioChannel is the Redis channel the translator publishes
// translated PCM frames to, keyed by session + language.
func translateAudioChannel(sessionID, lang string) string {
	return "goonj:live:" + sessionID + ":tts:" + lang
}

// serveTranslateAudio upgrades GET /ws/translate/{session}/{lang} and pumps
// binary PCM frames until either side closes. Auth uses the same
// access_token query param as the room socket.
func (h *hub) serveTranslateAudio(w http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	// Path: /ws/translate/{session}/{lang}
	const prefix = "/ws/translate/"
	rest := r.URL.Path[len(prefix):]
	var sessionID, lang string
	if i := indexByte(rest, '/'); i > 0 {
		sessionID, lang = rest[:i], rest[i+1:]
	}
	if sessionID == "" || lang == "" {
		http.Error(w, "missing session or language", http.StatusBadRequest)
		return
	}
	if _, _, err := validateToken(r.URL.Query().Get("access_token")); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, acceptOptions())
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	sub := h.rdb.Subscribe(ctx, translateAudioChannel(sessionID, lang))
	defer sub.Close()

	// Reader goroutine: treat any client text frame as a ping/keepalive and
	// answer with a pong so proxies keep the socket open.
	go func() {
		for {
			mt, _, err := conn.Read(ctx)
			if err != nil {
				cancel()
				return
			}
			if mt == websocket.MessageText {
				wctx, wcancel := context.WithTimeout(ctx, 3*time.Second)
				_ = conn.Write(wctx, websocket.MessageText, []byte(`{"type":"pong"}`))
				wcancel()
			}
		}
	}()

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			// Translator frames carry raw PCM in msg.Payload (binary over
			// Redis). If a JSON envelope sneaks in, forward it as text so
			// debug clients still see control events.
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			err := conn.Write(wctx, websocket.MessageBinary, []byte(msg.Payload))
			wcancel()
			if err != nil {
				return
			}
		}
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
