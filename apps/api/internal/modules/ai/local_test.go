package ai

import (
	"context"
	"math"
	"testing"
)

func TestHashEmbedderDeterministicAndNormalized(t *testing.T) {
	e := NewHashEmbedder(256)
	ctx := context.Background()

	a, err := e.Embed(ctx, []string{"the quick brown fox"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	b, err := e.Embed(ctx, []string{"the quick brown fox"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("want 1 vector each, got %d and %d", len(a), len(b))
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("non-deterministic at %d: %v vs %v", i, a[0][i], b[0][i])
		}
	}
	var norm float64
	for _, x := range a[0] {
		norm += float64(x) * float64(x)
	}
	if math.Abs(norm-1) > 1e-5 {
		t.Fatalf("vector not normalized: %f", norm)
	}
}

// TestHashEmbedderNeverZeroVector guards the pgvector NaN trap: a token-free
// text (".", punctuation, whitespace) would normalize to the zero vector,
// and cosine distance against zero is NaN. NaN ranks then break JSON
// encoding (zero-byte 200 responses) and pollute ANN indexes.
func TestHashEmbedderNeverZeroVector(t *testing.T) {
	e := NewHashEmbedder(256)
	vecs, err := e.Embed(context.Background(), []string{".", "   ", "... .", ""})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	for i, v := range vecs {
		var norm float64
		for _, x := range v {
			norm += float64(x) * float64(x)
		}
		if math.Abs(norm-1) > 1e-5 {
			t.Fatalf("case %d: zero/degenerate vector (norm=%f) — must be a unit vector", i, norm)
		}
	}
}

func TestHashEmbedderSimilarTextsCloserThanDissimilar(t *testing.T) {
	e := NewHashEmbedder(256)
	vecs, err := e.Embed(context.Background(), []string{
		"today we talk about live audio broadcasting",
		"broadcasting live audio is today's topic",
		"quantum computing reshapes cryptographic systems",
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	dot := func(x, y []float32) float64 {
		var d float64
		for i := range x {
			d += float64(x[i]) * float64(y[i])
		}
		return d
	}
	if sim, diff := dot(vecs[0], vecs[1]), dot(vecs[0], vecs[2]); sim <= diff {
		t.Fatalf("paraphrase similarity %f not above unrelated %f", sim, diff)
	}
}
