// Package translation implements live translated captions for live sessions:
// the creator enables per-session translation with target languages, a
// server-side translator joins the LiveKit room and streams the creator's
// audio through one OpenAI Realtime translation session per language, and
// caption deltas fan out over the existing room channel (Redis → ws-gateway)
// so listeners see translated text while the speaker is still talking.
package translation

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Caption is one timed caption line in a specific language.
type Caption struct {
	Lang    string `json:"lang"`
	Text    string `json:"text"`
	StartMs int    `json:"start_ms"`
	EndMs   int    `json:"end_ms"`
	IsFinal bool   `json:"is_final"`
}

// Store persists translation settings and caption history.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Settings is the per-session translation configuration.
type Settings struct {
	SessionID   string   `json:"session_id"`
	SourceLang  string   `json:"source_lang"`
	TargetLangs []string `json:"target_langs"`
	Enabled     bool     `json:"enabled"`
}

// GetSettings returns the translation config for a session (zero value when
// never configured).
func (s *Store) GetSettings(ctx context.Context, sessionID string) (*Settings, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT session_id::text, source_lang, target_langs, enabled
		FROM live_translations WHERE session_id = $1`, sessionID)
	var st Settings
	var langs []string
	err := row.Scan(&st.SessionID, &st.SourceLang, &langs, &st.Enabled)
	if err != nil {
		// No row yet: translation has never been configured for this session.
		return &Settings{SessionID: sessionID, TargetLangs: []string{}}, nil
	}
	st.TargetLangs = langs
	if st.TargetLangs == nil {
		st.TargetLangs = []string{}
	}
	return &st, nil
}

// UpsertSettings writes the translation config for a session.
func (s *Store) UpsertSettings(ctx context.Context, st *Settings) error {
	if st.TargetLangs == nil {
		st.TargetLangs = []string{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO live_translations (session_id, source_lang, target_langs, enabled, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (session_id) DO UPDATE
		SET source_lang = EXCLUDED.source_lang,
			target_langs = EXCLUDED.target_langs,
			enabled = EXCLUDED.enabled,
			updated_at = now()`,
		st.SessionID, st.SourceLang, st.TargetLangs, st.Enabled)
	return err
}

// SaveCaption persists one caption line (best-effort history; delivery never
// depends on this succeeding).
func (s *Store) SaveCaption(ctx context.Context, sessionID string, c Caption) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO live_captions (session_id, lang, text, start_ms, end_ms, is_final)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		sessionID, c.Lang, c.Text, c.StartMs, c.EndMs, c.IsFinal)
	return err
}

// SaveSourceCaption persists one source-language caption line.
func (s *Store) SaveSourceCaption(ctx context.Context, sessionID string, c Caption) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO live_captions_source (session_id, text, start_ms, end_ms, is_final)
		VALUES ($1, $2, $3, $4, $5)`,
		sessionID, c.Text, c.StartMs, c.EndMs, c.IsFinal)
	return err
}

// History returns the last 100 captions for a session in one language
// (newest last) — used when a listener joins or switches language mid-show.
func (s *Store) History(ctx context.Context, sessionID, lang string, limit int) ([]Caption, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT text, start_ms, end_ms, is_final
		FROM live_captions
		WHERE session_id = $1 AND lang = $2 AND is_final
		ORDER BY start_ms DESC
		LIMIT $3`, sessionID, lang, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Caption{}
	for rows.Next() {
		var c Caption
		if err := rows.Scan(&c.Text, &c.StartMs, &c.EndMs, &c.IsFinal); err != nil {
			return nil, err
		}
		c.Lang = lang
		out = append(out, c)
	}
	// Reverse to chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

// HistorySource returns the last source-language captions (chronological).
func (s *Store) HistorySource(ctx context.Context, sessionID string, limit int) ([]Caption, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT text, start_ms, end_ms, is_final
		FROM live_captions_source
		WHERE session_id = $1 AND is_final
		ORDER BY start_ms DESC
		LIMIT $2`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Caption{}
	for rows.Next() {
		var c Caption
		if err := rows.Scan(&c.Text, &c.StartMs, &c.EndMs, &c.IsFinal); err != nil {
			return nil, err
		}
		c.Lang = "src"
		out = append(out, c)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}
