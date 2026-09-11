package ai

// Default provider selection shared by every consumer. The embedding worker
// (document side) and the semantic search endpoint (query side) MUST use the
// same embedder or vectors are incomparable — pick here, never locally.

// DefaultEmbedder returns the deployment's embedder: OpenAI when
// OPENAI_API_KEY is set, else the deterministic local hasher. The hash
// dimension must match the migration's vector(1536) column exactly, or
// inserts fail.
func DefaultEmbedder() Embedder {
	if e := NewOpenAIEmbedder(); e != nil {
		return e
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
