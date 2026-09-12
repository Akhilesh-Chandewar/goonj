// Package translation — server-side realtime translator.
//
// This is the engine behind the demo promise: a creator speaks one language,
// listeners hear and read another in near-realtime. The browser-WebRTC path
// (TranslationStudio) covers self-serve creators; this worker covers the
// always-on path where Goonj operates the translation server-side:
//
//	creator mic → LiveKit SFU
//	  → TrackEgress (raw PCM over WS, pcm_s16le, 48kHz mono)
//	    → PCMPump (this process)
//	      → downsample 48k→24k (OpenAI Realtime's rate)
//	        → one OpenAI translation session per target language
//	          → transcript deltas → Redis room channel → listener caption bar
//	          → translated audio → (M4A/muted-view UI path; see listener doc)
//
// Everything is streaming: nothing is batched beyond one egress frame, so
// end-to-end latency stays in the sub-second class while the speaker talks.
package translation

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	lk "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/livekit"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/openai"
)

// roomChannel mirrors the live module's Redis convention.
func roomChannel(sessionID string) string { return "goonj:live:" + sessionID + ":ch" }

// Translator runs the always-on realtime translation for configured sessions.
type Translator struct {
	rdb    *redis.Client
	apiKey string
	log    *slog.Logger

	mu       sync.Mutex
	running  map[string]*translatorRun // keyed by session id
	pumpAddr string                    // host:port the PCMPump listens on (egress-reachable)
	live     LiveInfo
	egress   *lk.Egress // LiveKit client for StartTrackEgress (nil = disabled)
}

// LiveInfo resolves the LiveKit room + source track for a live session. The
// worker needs the track id to point TrackEgress at; the live module knows it
// from its streamer state.
type LiveInfo interface {
	// SourceTrack returns (roomName, trackID) for a live session, ok=false
	// when the session is not LIVE.
	SourceTrack(ctx context.Context, sessionID string) (room, trackID string, ok bool)
}

// translatorRun is one active session's translation state.
type translatorRun struct {
	sessionID string
	room      string
	trackID   string
	pump      *lk.PCMPump
	egressID  string
	sessions  map[string]*openai.TranslationSession // target lang → session
	cancel    context.CancelFunc
}

// NewTranslator builds the translator loop. pumpAddr is the egress-reachable
// host:port for the PCMPump (e.g. "worker:9600" in compose); egress is the
// LiveKit client used to start track egress (nil disables audio pumping).
func NewTranslator(rdb *redis.Client, apiKey, pumpAddr string, live LiveInfo, egress *lk.Egress, log *slog.Logger) *Translator {
	return &Translator{rdb: rdb, apiKey: apiKey, pumpAddr: pumpAddr, live: live, egress: egress, log: log, running: map[string]*translatorRun{}}
}

// Run subscribes to the control channel and applies configs until ctx ends.
// Multiple configs collapse naturally: each event fully reconciles that
// session's per-language sessions.
func (t *Translator) Run(ctx context.Context) {
	sub := t.rdb.Subscribe(ctx, ControlChannel)
	defer sub.Close()
	msgs := sub.Channel()
	t.log.Info("translator control loop started", slog.String("pump", t.pumpAddr))
	for {
		select {
		case <-ctx.Done():
			t.stopAll()
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			var ev ConfigEvent
			if json.Unmarshal([]byte(msg.Payload), &ev) != nil {
				continue
			}
			if err := t.apply(ctx, &ev); err != nil {
				t.log.Warn("translator apply failed",
					slog.String("session", ev.SessionID), slog.Any("error", err))
			}
		}
	}
}

// apply reconciles one session toward its config: start, edit, or stop.
func (t *Translator) apply(ctx context.Context, ev *ConfigEvent) error {
	t.mu.Lock()
	run, exists := t.running[ev.SessionID]
	t.mu.Unlock()

	if !ev.Enabled {
		if exists {
			t.stop(ev.SessionID)
		}
		return nil
	}

	// (Re)concile target set: stop langs no longer wanted, start new ones.
	if exists {
		return t.reconcile(ctx, run, ev)
	}

	room, trackID, ok := t.live.SourceTrack(ctx, ev.SessionID)
	if !ok || room == "" || trackID == "" {
		return errNotLive
	}

	runCtx, cancel := context.WithCancel(ctx)
	run = &translatorRun{
		sessionID: ev.SessionID,
		room:      room,
		trackID:   trackID,
		sessions:  map[string]*openai.TranslationSession{},
		cancel:    cancel,
	}
	t.mu.Lock()
	t.running[ev.SessionID] = run
	t.mu.Unlock()
	return t.startRun(runCtx, run, ev)
}

var errNotLive = errStatic("session is not live")

type errStatic string

func (e errStatic) Error() string { return string(e) }

// startRun boots the pump + egress + per-language sessions for a run.
func (t *Translator) startRun(ctx context.Context, run *translatorRun, ev *ConfigEvent) error {
	pump := lk.NewPCMPump(":"+pumpPort, t.log)
	run.pump = pump

	// The egress service must reach the pump; pumpAddr is that DNS name.
	egressID, err := t.startEgress(ctx, run, lk.URL(t.pumpAddr))
	if err != nil {
		pump.Stop(ctx)
		return err
	}
	run.egressID = egressID

	// OpenAI sessions per language.
	for _, lang := range ev.TargetLangs {
		if err := t.startLang(ctx, run, lang); err != nil {
			t.log.Warn("translation session start failed",
				slog.String("session", run.sessionID), slog.String("lang", lang), slog.Any("error", err))
		}
	}

	go t.pumpLoop(ctx, run, ev)
	t.log.Info("translation running",
		slog.String("session", run.sessionID), slog.String("room", run.room), slog.Any("targets", ev.TargetLangs))
	return nil
}

// startEgress points LiveKit's TrackEgress at the pump with retries: the
// track may not be published yet when a creator enables translation mid-room.
func (t *Translator) startEgress(ctx context.Context, run *translatorRun, wsURL string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		id, err := t.startEgressOnce(ctx, run, wsURL)
		if err == nil {
			return id, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 2 * time.Second):
		}
	}
	return "", lastErr
}

// pumpLoop drains PCMPump frames into every language session and reacts to
// mute events (sessions still receive silence — OpenAI handles it fine —
// this loop just logs for observability). Exits when ctx is cancelled.
func (t *Translator) pumpLoop(ctx context.Context, run *translatorRun, ev *ConfigEvent) {
	defer t.stop(run.sessionID)
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-run.pump.Frames():
			if !ok {
				return // egress disconnected: speaker left / track unpublished
			}
			pcm24 := downsample48to24(frame)
			t.mu.Lock()
			sessions := make([]*openai.TranslationSession, 0, len(run.sessions))
			for _, s := range run.sessions {
				sessions = append(sessions, s)
			}
			t.mu.Unlock()
			for _, s := range sessions {
				if err := s.AppendAudio(pcm24); err != nil {
					t.log.Debug("audio append failed", slog.Any("error", err))
				}
			}
		}
	}
}

// startLang opens one OpenAI translation session for a target language and
// wires its callbacks to the room channel + translated-audio stream.
func (t *Translator) startLang(ctx context.Context, run *translatorRun, lang string) error {
	sess, err := openai.NewTranslationSession(t.apiKey, "", lang, openai.TranslationCallbacks{
		OnTranslated: func(text string) {
			// Partial: fan out immediately — no final-gating.
			t.publishCaption(run.sessionID, lang, text, false, 0, 0)
		},
		OnTranslatedDone: func(text string, l string) {
			t.publishCaption(run.sessionID, l, text, true, 0, 0)
		},
		// Translated speech: publish PCM frames so listeners can HEAR the
		// translation (ws-gateway /ws/translate/{session}/{lang} relays these
		// to browsers, which buffer-play via WebAudio).
		OnAudio: func(pcm24k []byte) {
			t.publishAudio(run.sessionID, lang, pcm24k)
		},
		OnError: func(err error) {
			t.log.Warn("translation session error",
				slog.String("session", run.sessionID), slog.String("lang", lang), slog.Any("error", err))
		},
	})
	if err != nil {
		return err
	}
	t.mu.Lock()
	// Close any stale session for this language (reconcile path).
	if old, ok := run.sessions[lang]; ok {
		old.CloseNow()
	}
	run.sessions[lang] = sess
	t.mu.Unlock()
	return nil
}

// reconcile diffs the running set against the new config (languages can be
// added/removed live from the studio).
func (t *Translator) reconcile(ctx context.Context, run *translatorRun, ev *ConfigEvent) error {
	want := map[string]bool{}
	for _, l := range ev.TargetLangs {
		want[l] = true
	}
	t.mu.Lock()
	have := make([]string, 0, len(run.sessions))
	for l := range run.sessions {
		have = append(have, l)
	}
	t.mu.Unlock()
	for _, l := range have {
		if !want[l] {
			t.stopLang(run, l)
		}
	}
	for _, l := range ev.TargetLangs {
		t.mu.Lock()
		_, has := run.sessions[l]
		t.mu.Unlock()
		if !has {
			if err := t.startLang(ctx, run, l); err != nil {
				t.log.Warn("reconcile start failed", slog.String("lang", l), slog.Any("error", err))
			}
		}
	}
	return nil
}

func (t *Translator) stopLang(run *translatorRun, lang string) {
	t.mu.Lock()
	sess, ok := run.sessions[lang]
	delete(run.sessions, lang)
	t.mu.Unlock()
	if ok {
		_ = sess.Close() // drain pending output so the tail is not cut off
	}
}

// stop tears a session's whole translation apparatus down.
func (t *Translator) stop(sessionID string) {
	t.mu.Lock()
	run, ok := t.running[sessionID]
	delete(t.running, sessionID)
	t.mu.Unlock()
	if !ok {
		return
	}
	run.cancel()
	for lang := range run.sessions {
		t.stopLang(run, lang)
	}
	if run.pump != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		run.pump.Stop(ctx)
		cancel()
	}
	_ = run.egressID // egress ends itself when the pump socket closes
	t.log.Info("translation stopped", slog.String("session", sessionID))
}

func (t *Translator) stopAll() {
	t.mu.Lock()
	ids := make([]string, 0, len(t.running))
	for id := range t.running {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	for _, id := range ids {
		t.stop(id)
	}
}

// publishCaption pushes one caption event to the room channel — the same
// envelope the browser-WebRTC path uses, so listeners need no special case.
func (t *Translator) publishCaption(sessionID, lang, text string, final bool, startMs, endMs int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	env := CaptionEnvelope{SessionID: sessionID, Lang: lang, Text: text, StartMs: startMs, EndMs: endMs, Final: final}
	raw, err := json.Marshal(map[string]any{"type": "caption", "payload": env})
	if err != nil {
		return
	}
	if err := t.rdb.Publish(context.Background(), roomChannel(sessionID), raw).Err(); err != nil {
		t.log.Warn("caption publish failed", slog.String("session", sessionID), slog.Any("error", err))
	}
}

// publishAudio pushes one translated PCM frame to the per-language audio
// channel. Best-effort: a slow/absent audience drops frames rather than
// backing the translator up (Redis pub/sub has no consumer buffer anyway —
// publishes to zero subscribers are near-free).
func (t *Translator) publishAudio(sessionID, lang string, pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	if err := t.rdb.Publish(context.Background(),
		"goonj:live:"+sessionID+":tts:"+lang, pcm).Err(); err != nil {
		t.log.Warn("audio publish failed", slog.String("session", sessionID), slog.Any("error", err))
	}
}

// pumpPort is the PCMPump listen port (exposed to livekit-egress in compose).
const pumpPort = "9600"
