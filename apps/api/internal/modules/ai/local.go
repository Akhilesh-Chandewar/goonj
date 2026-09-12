package ai

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"unicode"
)

// HashEmbedder is a deterministic, dependency-free embedder: bag-of-words
// hashed into a fixed-dimension L2-normalized vector (the "hashing trick").
// Quality is below a neural model but it is honest retrieval: similar texts
// land near each other, and the whole semantic pipeline (chunking, ANN
// index, cosine ranking) is exercised end to end. Swapping in OpenAI
// embeddings later changes no downstream code — only the Dim().
type HashEmbedder struct {
	dim int
}

// NewHashEmbedder builds a hashing embedder with the given dimension.
func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = 384
	}
	return &HashEmbedder{dim: dim}
}

func (h *HashEmbedder) Dim() int    { return h.dim }
func (h *HashEmbedder) Name() string { return fmt.Sprintf("hash-embed-%d", h.dim) }

// Embed hashes word n-grams into the vector. Deterministic across runs and
// machines (FNV-1a), so re-embedding an episode never churns vectors.
func (h *HashEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for _, text := range texts {
		vec := make([]float32, h.dim)
		words := tokenize(text)
		for i, w := range words {
			vec[bucket(w, h.dim)] += 1
			// Light bigram signal for phrase sensitivity: exactly one bigram
			// per position, or weights grow O(n²) and swamp the unigrams.
			if i+1 < len(words) {
				vec[bucket(w+"_"+words[i+1], h.dim)] += 0.5
			}
		}
		normalize(vec)
		// A token-free text (".", pure punctuation, whitespace) normalizes to
		// the zero vector. Cosine distance against zero is NaN in pgvector, and
		// NaN poisons downstream JSON (encoding/json refuses it — callers then
		// write empty 200 responses). Deterministic unit vector instead.
		if !hasNonZero(vec) {
			vec[0] = 1
		}
		out = append(out, vec)
	}
	return out, nil
}

func hasNonZero(vec []float32) bool {
	for _, v := range vec {
		if v != 0 {
			return true
		}
	}
	return false
}

func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func bucket(token string, dim int) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(token))
	return int(h.Sum32() % uint32(dim))
}

func normalize(v []float32) {
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
}

// ExtractiveSummarizer picks the sentences closest to the transcript's
// centroid (a tiny TextRank-style baseline). No external calls, deterministic.
type ExtractiveSummarizer struct{}

func NewExtractiveSummarizer() *ExtractiveSummarizer { return &ExtractiveSummarizer{} }

func (s *ExtractiveSummarizer) Name() string { return "extractive-v1" }

// Summarize returns up to maxSentences sentences ranked by similarity to the
// document centroid, in original order.
func (s *ExtractiveSummarizer) Summarize(_ context.Context, transcript string) (string, error) {
	const maxSentences = 3
	sentences := splitSentences(transcript)
	if len(sentences) == 0 {
		return "", nil
	}
	if len(sentences) <= maxSentences {
		return strings.Join(sentences, " "), nil
	}

	embedder := NewHashEmbedder(256)
	vecs, err := embedder.Embed(context.Background(), sentences)
	if err != nil {
		return "", err
	}
	centroid := make([]float32, len(vecs[0]))
	for _, v := range vecs {
		for i, x := range v {
			centroid[i] += x
		}
	}
	normalize(centroid)

	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(sentences))
	for i, v := range vecs {
		var dot float64
		for j, x := range v {
			dot += float64(x) * float64(centroid[j])
		}
		scores[i] = scored{idx: i, score: dot}
	}
	sort.Slice(scores, func(a, b int) bool { return scores[a].score > scores[b].score })
	pick := map[int]bool{}
	for i := 0; i < maxSentences && i < len(scores); i++ {
		pick[scores[i].idx] = true
	}
	var out []string
	for i, s := range sentences {
		if pick[i] {
			out = append(out, s)
		}
	}
	return strings.Join(out, " "), nil
}

func splitSentences(s string) []string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return nil
	}
	var out []string
	runes := []rune(s)
	start := 0
	for i, r := range runes {
		if (r == '.' || r == '!' || r == '?') && i+1 < len(runes) && runes[i+1] == ' ' {
			out = append(out, string(runes[start:i+1]))
			start = i + 2
		}
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}
