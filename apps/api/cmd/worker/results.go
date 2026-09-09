package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QualityFile is a processed variant to persist.
type QualityFile struct {
	Quality     string
	StorageKey  string
	BitrateKbps int
	SizeBytes   int64
	MimeType    string
}

// ResultStore writes worker outcomes to Postgres.
type ResultStore struct {
	pool *pgxpool.Pool
}

func NewResultStore(pool *pgxpool.Pool) *ResultStore {
	return &ResultStore{pool: pool}
}

// SetStatus moves an audio row to a new status.
func (r *ResultStore) SetStatus(ctx context.Context, id, status string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE audio SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	return err
}

// MarkReady records final metadata and publishes the audio.
func (r *ResultStore) MarkReady(ctx context.Context, id string, durationMs int, waveform []byte, sizeBytes int64, mimeType string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE audio SET status = 'READY',
			published_at = COALESCE(published_at, now()), updated_at = now()
		WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ctx.Err()
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO audio_metadata (audio_id, duration_ms, waveform, size_bytes, mime_type)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (audio_id) DO UPDATE
		SET duration_ms = EXCLUDED.duration_ms,
			waveform = EXCLUDED.waveform,
			size_bytes = EXCLUDED.size_bytes,
			mime_type = EXCLUDED.mime_type,
			updated_at = now()`,
		id, durationMs, waveform, sizeBytes, mimeType)
	return err
}

// AddFile records one processed quality variant.
func (r *ResultStore) AddFile(ctx context.Context, audioID string, f QualityFile) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO audio_files (audio_id, quality, storage_key, bitrate_kbps, size_bytes, mime_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (audio_id, quality) DO UPDATE
		SET storage_key = EXCLUDED.storage_key,
			bitrate_kbps = EXCLUDED.bitrate_kbps,
			size_bytes = EXCLUDED.size_bytes,
			mime_type = EXCLUDED.mime_type`,
		audioID, f.Quality, f.StorageKey, f.BitrateKbps, f.SizeBytes, f.MimeType)
	return err
}
