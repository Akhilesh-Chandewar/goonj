package audio

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TranscriptChunk is one timed span of an episode transcript.
type TranscriptChunk struct {
	Idx     int     `json:"idx"`
	StartMs int     `json:"start_ms"`
	EndMs   int     `json:"end_ms"`
	Speaker *string `json:"speaker,omitempty"` // reserved for diarization (Phase 5.5)
	Text    string  `json:"text"`
}

// AISummary is the model-generated episode blurb, when one exists.
type AISummary struct {
	Summary   string    `json:"summary"`
	Model     string    `json:"model"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Transcript is the full transcript payload for the /transcript endpoint.
type Transcript struct {
	AudioID string            `json:"audio_id"`
	Chunks  []TranscriptChunk `json:"chunks"`
	Summary *AISummary        `json:"summary,omitempty"`
}

// TranscriptStore reads transcript_chunks / audio_summaries (written by the
// embedding worker). Kept separate from Store: it has no audio-module
// dependencies and can be constructed anywhere the pool exists.
type TranscriptStore struct {
	pool *pgxpool.Pool
}

func NewTranscriptStore(pool *pgxpool.Pool) *TranscriptStore {
	return &TranscriptStore{pool: pool}
}

// Get returns the transcript and AI summary for an episode; ErrNotFound when
// no transcript has been produced yet.
func (ts *TranscriptStore) Get(ctx context.Context, audioID string) (*Transcript, error) {
	t := &Transcript{AudioID: audioID, Chunks: []TranscriptChunk{}}

	rows, err := ts.pool.Query(ctx, `
		SELECT idx, start_ms, end_ms, speaker, text
		FROM transcript_chunks WHERE audio_id = $1 ORDER BY idx`, audioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c TranscriptChunk
		var speaker string
		if err := rows.Scan(&c.Idx, &c.StartMs, &c.EndMs, &speaker, &c.Text); err != nil {
			return nil, err
		}
		if speaker != "" {
			c.Speaker = &speaker
		}
		t.Chunks = append(t.Chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var s AISummary
	err = ts.pool.QueryRow(ctx, `
		SELECT summary, model, updated_at
		FROM audio_summaries WHERE audio_id = $1`, audioID).
		Scan(&s.Summary, &s.Model, &s.UpdatedAt)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// no summary yet — fine
	case err != nil:
		return nil, err
	default:
		t.Summary = &s
	}

	if len(t.Chunks) == 0 && t.Summary == nil {
		return nil, ErrNotFound
	}
	return t, nil
}
