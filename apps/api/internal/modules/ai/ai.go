// Package ai defines Phase 5's provider interfaces: Transcriber, Embedder,
// and Summarizer. Implementations are swappable per PLAN §2 — the worker
// depends on the interfaces, never on a concrete vendor SDK. v1 ships two
// implementations each: a zero-dependency local one (whisper.cpp binary or
// hashing embeddings / extractive summaries) and OpenAI-backed ones enabled
// by env keys.
package ai

import "context"

// Segment is one span of transcribed speech.
type Segment struct {
	StartMs int
	EndMs   int
	Text    string
}

// Transcriber converts audio into timed text segments.
type Transcriber interface {
	// Transcribe returns ordered segments covering the audio at storageKey.
	// Implementations must be idempotent-friendly: no global state.
	Transcribe(ctx context.Context, storageKey string) ([]Segment, error)
	Name() string
}

// Embedder turns text into a fixed-dimension vector.
type Embedder interface {
	// Dim reports the vector dimension produced by Embed.
	Dim() int
	// Embed returns one vector per input text, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Name() string
}

// Summarizer condenses an episode transcript into a short blurb.
type Summarizer interface {
	Summarize(ctx context.Context, transcript string) (string, error)
	Name() string
}
