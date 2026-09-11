-- Phase 5 — AI & scale (part 1): transcripts, semantic search embeddings,
-- AI summaries. Requires the pgvector extension (compose now ships the
-- pgvector/pgvector:pg16 image; CREATE EXTENSION is idempotent).

-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;

-- Plain-text transcript, chunked for embedding alignment. One row per chunk
-- so embeddings match the span they describe; speakers land in Phase 5.5
-- (diarization) — the column is reserved now.
CREATE TABLE transcript_chunks (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    audio_id   UUID NOT NULL REFERENCES audio (id) ON DELETE CASCADE,
    idx        INTEGER NOT NULL CHECK (idx >= 0),
    start_ms   INTEGER NOT NULL CHECK (start_ms >= 0),
    end_ms     INTEGER NOT NULL CHECK (end_ms >= start_ms),
    speaker    TEXT NOT NULL DEFAULT '',
    text       TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 4000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (audio_id, idx)
);
CREATE INDEX idx_transcript_audio ON transcript_chunks (audio_id, idx);

-- One embedding per chunk (1536 = OpenAI text-embedding-3-small dimension;
-- the column type is chosen per deployment via EMBED_DIM at migration time
-- in prod — fixed here for dev parity). Halfvec-style half precision is not
-- used to keep pgvector 0.5+ compatibility simple.
CREATE TABLE chunk_embeddings (
    chunk_id   UUID PRIMARY KEY REFERENCES transcript_chunks (id) ON DELETE CASCADE,
    embedding  vector(1536) NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Covering index for approximate nearest neighbour search (cosine distance).
-- Queries MUST filter joins through audio for visibility; the index only
-- accelerates distance ordering.
CREATE INDEX idx_chunk_embeddings_ann ON chunk_embeddings
    USING hnsw (embedding vector_cosine_ops);

-- AI-generated episode summary (one per audio, regenerated on re-transcribe).
CREATE TABLE audio_summaries (
    audio_id   UUID PRIMARY KEY REFERENCES audio (id) ON DELETE CASCADE,
    summary    TEXT NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS audio_summaries;
DROP TABLE IF EXISTS chunk_embeddings;
DROP TABLE IF EXISTS transcript_chunks;
