package translation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/openai"
)

// Browser-side translation: the creator's browser opens a WebRTC peer
// connection straight to OpenAI's Realtime translation endpoint using an
// ephemeral client secret minted here. Audio never transits Goonj servers —
// the fastest possible path (browser → OpenAI). Caption events arrive on the
// browser's oai-events data channel; the creator's client relays them to
// /ingest below, which fans out through Redis to every listener.

// BrowserSessionRequest is the body for POST /live/{id}/translate/session.
type BrowserSessionRequest struct {
	TargetLang string `json:"target_lang"`
	SourceLang string `json:"source_lang,omitempty"`
}

// BrowserSessionResponse carries what the browser needs to open its peer
// connection and listen for events.
type BrowserSessionResponse struct {
	ClientSecret string    `json:"client_secret"`
	ExpiresAt    time.Time `json:"expires_at"`
	Model        string    `json:"model"`
	// WebRTCURL is OpenAI's translation SDP exchange endpoint.
	WebRTCURL string `json:"webrtc_url"`
	// EventsChannel is the WebRTC data channel name carrying session events.
	EventsChannel string `json:"events_channel"`
	TargetLang    string `json:"target_lang"`
}

// MintBrowserSession creates the ephemeral translation session for the
// creator's browser. One session per call; the creator requests one per
// target language (the UI offers the current config's languages).
func (s *Service) MintBrowserSession(ctx context.Context, sessionID string, req BrowserSessionRequest) (*BrowserSessionResponse, error) {
	st, err := s.Settings(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if !st.Enabled {
		return nil, ErrTranslationDisabled
	}
	lang := normalizeLang(req.TargetLang)
	if _, ok := SupportedLanguages[lang]; !ok {
		return nil, fmt.Errorf("%w: %q", ErrInvalidLanguage, lang)
	}
	// The target must be part of the session's configured set (cost control).
	found := false
	for _, l := range st.TargetLangs {
		if l == lang {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: %q not enabled for this session", ErrInvalidLanguage, lang)
	}

	resp, err := openai.MintTranslationClientSecret(ctx, s.apiKey, lang)
	if err != nil {
		return nil, err
	}
	return &BrowserSessionResponse{
		ClientSecret:  resp.ClientSecret.Value,
		ExpiresAt:     resp.ClientSecret.ExpiresAt,
		Model:         resp.Model,
		// OpenAI's WebRTC SDP exchange endpoint for translation calls.
		WebRTCURL:     "https://api.openai.com/v1/realtime/translations/calls",
		EventsChannel: "oai-events",
		TargetLang:    lang,
	}, nil
}

// IngestCaption receives one caption event relayed by the creator's browser
// (from the OpenAI data channel) and fans it out to the room. Requires the
// creator's own auth; trust comes from the authenticated caller, the payload
// is sanitized (length caps) before broadcast.
func (s *Service) IngestCaption(ctx context.Context, sessionID, lang, text string, final bool, startMs, endMs int) error {
	if text == "" {
		return nil
	}
	if len(text) > 2000 {
		text = text[:2000]
	}
	if lang == "" {
		lang = "src"
	}
	env := CaptionEnvelope{
		SessionID: sessionID,
		Lang:      normalizeLang(lang),
		Text:      text,
		StartMs:   startMs,
		EndMs:     endMs,
		Final:     final,
	}
	raw, err := json.Marshal(map[string]any{"type": "caption", "payload": env})
	if err != nil {
		return err
	}
	if err := s.rdb.Publish(ctx, "goonj:live:"+sessionID+":ch", raw).Err(); err != nil {
		return err
	}
	// Persist finals best-effort (off the hot path).
	if env.Final {
		go func(c Caption) {
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if c.Lang == "src" || c.Lang == "" {
				_ = s.store.SaveSourceCaption(bg, sessionID, c)
				return
			}
			_ = s.store.SaveCaption(bg, sessionID, c)
		}(Caption{Lang: env.Lang, Text: env.Text, StartMs: env.StartMs, EndMs: env.EndMs, IsFinal: true})
	}
	s.log.Debug("caption ingested",
		slog.String("session", sessionID), slog.String("lang", env.Lang), slog.Bool("final", env.Final))
	return nil
}
