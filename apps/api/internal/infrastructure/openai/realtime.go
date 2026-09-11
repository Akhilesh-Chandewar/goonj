// Package openai provides the OpenAI Realtime translation client used by the
// translator service: one streaming session per (source track, target
// language). The protocol is the dedicated translation endpoint
// (/v1/realtime/translations), which streams translated audio and transcript
// deltas while the speaker is still talking.
//
// Latency notes baked into this client:
//   - audio is forwarded as it arrives (24kHz PCM16, ~40ms frames), never
//     batched beyond what the WS can absorb;
//   - transcript deltas are published the moment they arrive (no
//     final-text gating);
//   - session.close triggers a drain: pending output events are read until
//     session.closed so the tail of a stream is not cut off.
package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// TranslationHTTPBase is the REST base for client-secret minting.
const TranslationHTTPBase = "https://api.openai.com/v1/realtime/translations"

// TranslationClientSecret is the minted ephemeral credential for a browser
// WebRTC translation session.
type TranslationClientSecret struct {
	ClientSecret struct {
		Value     string    `json:"value"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"client_secret"`
	Model string `json:"model"`
}

// MintTranslationClientSecret creates a short-lived translation session
// secret for one target language. The browser exchanges its SDP offer with
// this secret directly against OpenAI — audio never transits our servers.
func MintTranslationClientSecret(ctx context.Context, apiKey, targetLang string) (*TranslationClientSecret, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("openai: no api key configured")
	}
	body := map[string]any{
		"session": map[string]any{
			"model": "gpt-realtime-translate",
			"audio": map[string]any{
				"output": map[string]any{"language": targetLang},
			},
		},
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		TranslationHTTPBase+"/client_secrets", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OpenAI-Safety-Identifier", "goonj-live-translation")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai client_secrets: %w", err)
	}
	defer resp.Body.Close()
	rawResp, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai client_secrets: %s: %.300s", resp.Status, rawResp)
	}
	var out TranslationClientSecret
	if err := json.Unmarshal(rawResp, &out); err != nil {
		return nil, fmt.Errorf("decode client secret: %w", err)
	}
	return &out, nil
}

// RealtimeBase is the OpenAI Realtime endpoint base URL. A var (not const)
// so tests can point it at a fake server and self-hosted deployments can
// target a compatible gateway.
var RealtimeBase = "wss://api.openai.com/v1/realtime"

// TranslationSession is one live translation stream for one target language.
// Methods are safe for concurrent use; writes serialize on the socket.
type TranslationSession struct {
	conn   *websocket.Conn
	wmu    sync.Mutex
	lang   string
	model  string
	writeExp time.Duration

	onSourceDelta    func(text string)      // speaker's language (input transcript)
	onTranslated     func(text string)      // target-language transcript delta
	onTranslatedDone func(text string, lang string) // per-turn final (best-effort)
	onAudio          func(pcm24k []byte)    // translated audio, PCM16 24kHz mono
	onError          func(err error)
}

// TranslationCallbacks receives session events.
type TranslationCallbacks struct {
	OnSourceDelta    func(string)
	OnTranslated     func(string)
	OnTranslatedDone func(text string, lang string)
	OnAudio          func(pcm24k []byte)
	OnError          func(error)
}

// NewTranslationSession dials the translation endpoint, configures the target
// language, and starts the read pump. apiKey comes from env; model is
// gpt-realtime-translate by default.
func NewTranslationSession(apiKey, model, targetLang string, cb TranslationCallbacks) (*TranslationSession, error) {
	if model == "" {
		model = "gpt-realtime-translate"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	url := RealtimeBase + "/translations?model=" + model
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: map[string][]string{
			"Authorization":             {"Bearer " + apiKey},
			"OpenAI-Safety-Identifier":  {"goonj-live-translation"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("openai realtime dial: %w", err)
	}
	conn.SetReadLimit(1 << 24)

	s := &TranslationSession{
		conn:     conn,
		lang:     targetLang,
		model:    model,
		writeExp: 10 * time.Second,
	}
	// Configure the session before audio flows (one round trip, then start).
	if err := s.send(map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"audio": map[string]any{
				"output": map[string]any{"language": targetLang},
			},
		},
	}); err != nil {
		conn.Close(websocket.StatusInternalError, "config failed")
		return nil, fmt.Errorf("openai realtime session.update: %w", err)
	}
	go s.readPump(cb)
	return s, nil
}

// AppendAudio streams one PCM16 24kHz mono chunk (base64 on the wire).
func (s *TranslationSession) AppendAudio(pcm []byte) error {
	return s.send(map[string]any{
		"type":  "session.input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(pcm),
	})
}

// Close drains: session.close makes the service flush pending output, then
// emits session.closed. We keep reading until that event or a timeout.
func (s *TranslationSession) Close() error {
	_ = s.send(map[string]any{"type": "session.close"})
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, raw, err := s.conn.Read(drainCtx)
		if err != nil {
			return nil // socket gone: nothing more to drain
		}
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &ev) == nil && ev.Type == "session.closed" {
			break
		}
	}
	return s.conn.Close(websocket.StatusNormalClosure, "done")
}

// CloseNow abandons the session without draining.
func (s *TranslationSession) CloseNow() {
	_ = s.conn.Close(websocket.StatusInternalError, "abandoned")
}

func (s *TranslationSession) send(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), s.writeExp)
	defer cancel()
	return s.conn.Write(ctx, websocket.MessageText, raw)
}

func (s *TranslationSession) readPump(cb TranslationCallbacks) {
	for {
		_, raw, err := s.conn.Read(context.Background()) // long-lived stream
		if err != nil {
			if cb.OnError != nil && !strings.Contains(err.Error(), "closed") {
				cb.OnError(err)
			}
			return
		}
		s.dispatch(raw, cb)
	}
}

func (s *TranslationSession) dispatch(raw []byte, cb TranslationCallbacks) {
	var ev struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
		Audio string `json:"audio"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		return
	}
	switch ev.Type {
	case "session.input_transcript.delta":
		if cb.OnSourceDelta != nil && ev.Delta != "" {
			cb.OnSourceDelta(ev.Delta)
		}
	case "session.output_transcript.delta":
		if cb.OnTranslated != nil && ev.Delta != "" {
			cb.OnTranslated(ev.Delta)
		}
	case "session.output_transcript.done":
		if cb.OnTranslatedDone != nil {
			cb.OnTranslatedDone(ev.Text, s.lang)
		}
	case "session.output_audio.delta":
		if cb.OnAudio != nil && ev.Audio != "" {
			if raw, err := base64.StdEncoding.DecodeString(ev.Audio); err == nil {
				cb.OnAudio(raw)
			}
		}
	}
}
