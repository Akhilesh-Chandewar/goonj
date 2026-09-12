package ai

import "os"

// Default provider selection shared by every consumer. The embedding worker
// (document side) and the semantic search endpoint (query side) MUST use the
// same embedder or vectors are incomparable — pick here, never locally.

// DefaultEmbedder returns the deployment's embedder: OpenAI-compatible when
// OPENAI_API_KEY is set and embeddings are not disabled via
// EMBEDDINGS_DISABLED=1, else the deterministic local hasher. The hash
// dimension must match the migration's vector(1536) column exactly, or
// inserts fail.
//
// The disable flag exists for providers that speak the OpenAI chat/STT API
// but serve no /embeddings endpoint (e.g. Groq) — without it, merely setting
// the key would break semantic-search indexing.
func DefaultEmbedder() Embedder {
	if os.Getenv("EMBEDDINGS_DISABLED") != "1" {
		if e := NewOpenAIEmbedder(); e != nil {
			return e
		}
	}
	return NewHashEmbedder(1536)
}

// DefaultSummarizer returns the OpenAI chat summarizer when configured,
// else the extractive baseline so summaries work without any keys.
func DefaultSummarizer() Summarizer {
	if s := NewOpenAISummarizer(); s != nil {
		return s
	}
	return NewExtractiveSummarizer()
}

// DefaultTranscriber returns OpenAI whisper when a key is present, else the
// whisper.cpp wrapper (which reports ErrUnavailable at Transcribe time when
// its binary/model env vars are unset — callers skip gracefully).
func DefaultTranscriber() Transcriber {
	if t := NewOpenAITranscriber(); t != nil {
		return t
	}
	return NewLocalTranscriber()
}
