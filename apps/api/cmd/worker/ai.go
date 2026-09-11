package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/ai"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/shared/tasks"
)

// Chunking constants: transcripts are stored in ~600-character chunks so the
// embedding of a chunk describes a coherent span a listener can seek to. The
// hard max matches transcript_chunks.text's CHECK constraint.
const (
	chunkTargetRunes = 600
	chunkHardMax     = 4000
)

// aiStore is the worker-side writer for transcript_chunks, chunk_embeddings
// and audio_summaries (schema: migrations/00006_ai_semantic.sql).
type aiStore struct {
	pool *pgxpool.Pool
}

// ReplaceTranscript swaps the chunk list for an audio id in one transaction;
// retries replace rather than append, keeping the task idempotent.
func (s *aiStore) ReplaceTranscript(ctx context.Context, audioID string, segs []ai.Segment) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM transcript_chunks WHERE audio_id = $1`, audioID); err != nil {
		return err
	}
	for i, seg := range segs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO transcript_chunks (audio_id, idx, start_ms, end_ms, text)
			VALUES ($1, $2, $3, $4, $5)`,
			audioID, i, seg.StartMs, seg.EndMs, seg.Text); err != nil {
		return err
	}
	}
	return tx.Commit(ctx)
}

// Chunk is one stored transcript span awaiting embedding.
type Chunk struct {
	ID   string
	Text string
}

// ChunksWithoutVectors returns chunks lacking an embedding row; re-runs only
// embed what is missing, keeping the task idempotent.
func (s *aiStore) ChunksWithoutVectors(ctx context.Context, audioID string) ([]Chunk, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.text
		FROM transcript_chunks c
		LEFT JOIN chunk_embeddings e ON e.chunk_id = c.id
		WHERE c.audio_id = $1 AND e.chunk_id IS NULL
		ORDER BY c.idx`, audioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertEmbeddings stores one vector per chunk id, all tagged with model.
// Vectors go in as pgvector text literals cast to ::vector — pgx would
// otherwise encode []float32 as float4[], which vector(1536) rejects.
func (s *aiStore) InsertEmbeddings(ctx context.Context, model string, ids []string, vecs [][]float32) error {
	batch := &pgx.Batch{}
	for i, id := range ids {
		batch.Queue(`
			INSERT INTO chunk_embeddings (chunk_id, embedding, model)
			VALUES ($1, $2::vector, $3)
			ON CONFLICT (chunk_id) DO UPDATE
			SET embedding = EXCLUDED.embedding, model = EXCLUDED.model, created_at = now()`,
			id, ai.VectorLiteral(vecs[i]), model)
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// SetSummary upserts the AI summary for an episode.
func (s *aiStore) SetSummary(ctx context.Context, audioID, summary, model string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audio_summaries (audio_id, summary, model, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (audio_id) DO UPDATE
		SET summary = EXCLUDED.summary, model = EXCLUDED.model, updated_at = now()`,
		audioID, summary, model)
	return err
}

// transcribeAudio runs STT over a READY episode and stores timed chunks.
// Graceful no-op when no transcriber is configured (ErrUnavailable): the task
// succeeds so static env config is not retried into the DLQ.
func (h *audioHandler) transcribeAudio(ctx context.Context, t *asynq.Task) error {
	payload, err := tasks.DecodeTranscribeAudio(t)
	if err != nil {
		return fmt.Errorf("decode payload: %w", err) // no retry on bad payload
	}
	h.log.Info("transcribing audio", slog.String("audio", payload.AudioID))

	transcriber := ai.DefaultTranscriber()
	if lt, ok := transcriber.(*ai.LocalTranscriber); ok && lt.Unconfigured() {
		h.log.Info("no transcriber configured; skipping transcription",
			slog.String("audio", payload.AudioID))
		return nil
	}

	// The payload carries a remote object key; both backends want a local
	// file (OpenAI posts it, whisper.cpp runs ffmpeg on it) — fetch first.
	workDir, err := os.MkdirTemp("", "goonj-stt-")
	if err != nil {
		return fmt.Errorf("create workdir: %w", err)
	}
	defer os.RemoveAll(workDir)
	audioPath := workDir + "/input"
	if err := h.download(ctx, payload.StorageKey, audioPath); err != nil {
		return fmt.Errorf("download for transcription: %w", err)
	}

	segs, err := transcriber.Transcribe(ctx, audioPath)
	if err != nil {
		if errors.Is(err, ai.ErrUnavailable) {
			h.log.Warn("transcriber unavailable; skipping",
				slog.String("audio", payload.AudioID))
			return nil
		}
		return fmt.Errorf("transcribe: %w", err)
	}

	chunks := chunkSegments(segs)
	if len(chunks) == 0 {
		h.log.Info("transcription produced no speech; nothing to store",
			slog.String("audio", payload.AudioID))
		return nil
	}
	if err := h.ai.ReplaceTranscript(ctx, payload.AudioID, chunks); err != nil {
		return fmt.Errorf("store transcript: %w", err)
	}

	// Transcription done → chain the embed step.
	task, err := tasks.NewEmbedAudio(tasks.EmbedAudioPayload{AudioID: payload.AudioID})
	if err != nil {
		return fmt.Errorf("build embed task: %w", err)
	}
	if _, err := h.enqueuer.EnqueueContext(ctx, task); err != nil {
		return fmt.Errorf("enqueue embed: %w", err)
	}

	h.log.Info("transcript stored",
		slog.String("audio", payload.AudioID),
		slog.Int("segments", len(segs)),
		slog.Int("chunks", len(chunks)))
	return nil
}

// embedAudio embeds chunks missing vectors, then regenerates the summary.
// Idempotent: only chunks without rows are embedded.
func (h *audioHandler) embedAudio(ctx context.Context, t *asynq.Task) error {
	payload, err := tasks.DecodeEmbedAudio(t)
	if err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	h.log.Info("embedding audio", slog.String("audio", payload.AudioID))

	embedder := ai.DefaultEmbedder()
	chunks, err := h.ai.ChunksWithoutVectors(ctx, payload.AudioID)
	if err != nil {
		return fmt.Errorf("load chunks: %w", err)
	}
	if len(chunks) > 0 {
		texts := make([]string, len(chunks))
		ids := make([]string, len(chunks))
		for i, c := range chunks {
			texts[i] = c.Text
			ids[i] = c.ID
		}
		vecs, err := embedder.Embed(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}
		if err := h.ai.InsertEmbeddings(ctx, embedder.Name(), ids, vecs); err != nil {
			return fmt.Errorf("store embeddings: %w", err)
		}
	}

	// Summary: fold the whole transcript (ordered chunks) into the model.
	var full strings.Builder
	rows, err := h.pool.Query(ctx, `
		SELECT text FROM transcript_chunks WHERE audio_id = $1 ORDER BY idx`,
		payload.AudioID)
	if err != nil {
		return fmt.Errorf("load transcript: %w", err)
	}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			rows.Close()
			return err
		}
		full.WriteString(text)
		full.WriteString(" ")
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	summarizer := ai.DefaultSummarizer()
	if summary, err := summarizer.Summarize(ctx, full.String()); err != nil {
		// Summary is a nice-to-have: log and continue; chunks are stored.
		h.log.Warn("summary generation failed",
			slog.String("audio", payload.AudioID), slog.Any("error", err))
	} else if summary != "" {
		if err := h.ai.SetSummary(ctx, payload.AudioID, summary, summarizer.Name()); err != nil {
			return fmt.Errorf("store summary: %w", err)
		}
	}

	h.log.Info("embeddings ready",
		slog.String("audio", payload.AudioID),
		slog.Int("embedded", len(chunks)),
		slog.String("model", embedder.Name()))
	return nil
}

// splitAtWords breaks s into pieces of at most max runes, cutting at spaces
// when possible so words survive intact.
func splitAtWords(s string, max int) []string {
	if max <= 0 {
		max = chunkTargetRunes
	}
	var out []string
	for utf8.RuneCountInString(s) > max {
		cut := max
		runes := []rune(s)
		for i := cut; i > max/2; i-- {
			if runes[i-1] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimRight(string(runes[:cut]), " "))
		s = string(runes[cut:])
	}
	if strings.TrimSpace(s) != "" {
		out = append(out, s)
	}
	return out
}

// chunkSegments merges ordered STT segments into ~chunkTargetRunes chunks so
// each embedding describes a coherent, seekable span. Timestamps are
// preserved from the first/last merged segment.
func chunkSegments(segs []ai.Segment) []ai.Segment {
	if len(segs) == 0 {
		return nil
	}
	merged := make([]ai.Segment, 0, len(segs)/4+1)
	var cur ai.Segment
	var size int
	for _, seg := range segs {
		if size > 0 && size+utf8.RuneCountInString(seg.Text) > chunkTargetRunes {
			merged = append(merged, cur)
			cur = ai.Segment{}
			size = 0
		}
		// One utterance longer than the target becomes its own pieces so a
		// single monologue segment does not dominate the embedding.
		if size == 0 && utf8.RuneCountInString(seg.Text) > chunkTargetRunes {
			for _, part := range splitAtWords(seg.Text, chunkTargetRunes) {
				merged = append(merged, ai.Segment{StartMs: seg.StartMs, EndMs: seg.EndMs, Text: part})
			}
			continue
		}
		if size == 0 {
			cur.StartMs = seg.StartMs
		}
		if cur.Text != "" {
			cur.Text += " "
		}
		cur.Text += seg.Text
		cur.EndMs = seg.EndMs
		size = utf8.RuneCountInString(cur.Text)
	}
	if strings.TrimSpace(cur.Text) != "" {
		merged = append(merged, cur)
	}

	// Respect the DB CHECK limit; split pathological oversized chunks.
	out := make([]ai.Segment, 0, len(merged))
	for _, c := range merged {
		for utf8.RuneCountInString(c.Text) > chunkHardMax {
			runes := []rune(c.Text)
			head := ai.Segment{StartMs: c.StartMs, EndMs: c.EndMs, Text: string(runes[:chunkHardMax])}
			out = append(out, head)
			c.Text = string(runes[chunkHardMax:])
			c.StartMs = c.EndMs // remainder keeps only the end timestamp
		}
		out = append(out, c)
	}
	return out
}
