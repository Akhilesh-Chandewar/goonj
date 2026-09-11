package translation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Control channel the translator worker subscribes to. The API publishes a
// ConfigEvent whenever a creator enables/disables/edits translation; the
// translator spins its per-language sessions up or down to match.
const ControlChannel = "goonj:translate:ctl"

// Supported target languages (ISO 639-1). Deliberately curated: each maps to
// a caption UI label, and the OpenAI translation session accepts them all.
var SupportedLanguages = map[string]string{
	"en": "English", "es": "Español", "fr": "Français", "de": "Deutsch",
	"hi": "हिन्दी", "pt": "Português", "zh": "中文", "ja": "日本語",
	"ko": "한국어", "it": "Italiano", "ru": "Русский", "ar": "العربية",
}

// MaxTargetLanguages caps per-session cost and fan-out complexity.
const MaxTargetLanguages = 3

// ConfigEvent is published on ControlChannel when translation config changes.
type ConfigEvent struct {
	SessionID   string   `json:"session_id"`
	Room        string   `json:"room"` // livekit room name ("live-<session>")
	SourceLang  string   `json:"source_lang"`
	TargetLangs []string `json:"target_langs"`
	Enabled     bool     `json:"enabled"`
}

// CaptionEnvelope is the room-channel wire format for caption fan-out.
type CaptionEnvelope struct {
	SessionID string `json:"session_id"`
	Lang      string `json:"lang"` // "src" for the speaker's language
	Text      string `json:"text"`
	StartMs   int    `json:"start_ms"`
	EndMs     int    `json:"end_ms"`
	Final     bool   `json:"final"`
}

// ErrInvalidLanguage when a language code is not supported.
var ErrInvalidLanguage = errors.New("unsupported language")

// ErrNoLanguages when enabling with an empty target set.
var ErrNoLanguages = errors.New("at least one target language required")

// ErrTooManyLanguages when the target set exceeds MaxTargetLanguages.
var ErrTooManyLanguages = errors.New("too many target languages")

// ErrTranslationDisabled when minting a session for a non-enabled session.
var ErrTranslationDisabled = errors.New("translation not enabled for this session")

// Service implements the translation control plane.
type Service struct {
	store  *Store
	rdb    *redis.Client
	apiKey string
	log    *slog.Logger
}

func NewService(store *Store, rdb *redis.Client, log *slog.Logger) *Service {
	return &Service{store: store, rdb: rdb, apiKey: os.Getenv("OPENAI_API_KEY"), log: log}
}

// Configure sets the translation config for a session and notifies the
// translator worker. The creator can edit while live; the translator applies
// deltas (new languages start fresh sessions, removed ones stop).
func (s *Service) Configure(ctx context.Context, sessionID, room string, st *Settings) (*Settings, error) {
	st.SessionID = sessionID
	st.SourceLang = normalizeLang(st.SourceLang)

	if st.Enabled {
		if len(st.TargetLangs) == 0 {
			return nil, ErrNoLanguages
		}
		if len(st.TargetLangs) > MaxTargetLanguages {
			return nil, ErrTooManyLanguages
		}
		clean := make([]string, 0, len(st.TargetLangs))
		for _, l := range st.TargetLangs {
			l = normalizeLang(l)
			if _, ok := SupportedLanguages[l]; !ok {
				return nil, fmt.Errorf("%w: %q", ErrInvalidLanguage, l)
			}
			clean = append(clean, l)
		}
		st.TargetLangs = dedupe(clean)
	} else {
		st.TargetLangs = nil // disabled: clear targets
	}

	if err := s.store.UpsertSettings(ctx, st); err != nil {
		return nil, err
	}

	ev := ConfigEvent{
		SessionID:   sessionID,
		Room:        room,
		SourceLang:  st.SourceLang,
		TargetLangs: st.TargetLangs,
		Enabled:     st.Enabled,
	}
	raw, _ := json.Marshal(ev)
	if err := s.rdb.Publish(ctx, ControlChannel, raw).Err(); err != nil {
		s.log.Error("translation control publish failed",
			slog.String("session", sessionID), slog.Any("error", err))
		// Settings are stored; the translator catches up on next edit or
		// restart. Surface the error so handlers can 503.
		return nil, err
	}
	s.log.Info("translation configured",
		slog.String("session", sessionID),
		slog.Bool("enabled", st.Enabled),
		slog.Any("targets", st.TargetLangs))
	return st, nil
}

// Settings returns the current config for a session.
func (s *Service) Settings(ctx context.Context, sessionID string) (*Settings, error) {
	return s.store.GetSettings(ctx, sessionID)
}

// History returns past captions for a language ("src" = speaker's language).
func (s *Service) History(ctx context.Context, sessionID, lang string, limit int) ([]Caption, error) {
	if lang == "src" || lang == "" {
		return s.store.HistorySource(ctx, sessionID, limit)
	}
	return s.store.History(ctx, sessionID, normalizeLang(lang), limit)
}

func normalizeLang(l string) string {
	return strings.ToLower(strings.TrimSpace(l))
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
