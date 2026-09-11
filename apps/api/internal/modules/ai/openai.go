package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// OpenAI-compatible settings read from env. Works with any base URL speaking
// the OpenAI API (OpenAI, Azure gateways, local llama.cpp servers).
type openaiConfig struct {
	apiKey string
	base   string
	model  string
	client *http.Client
}

func newOpenAIConfig(model string) openaiConfig {
	return openaiConfig{
		apiKey: os.Getenv("OPENAI_API_KEY"),
		base:   envOr("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		model:  model,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (c openaiConfig) enabled() bool { return c.apiKey != "" }

// post issues one OpenAI call and decodes the response into out.
func (c openaiConfig) post(ctx context.Context, path string, req, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<22))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("openai %s: %s: %.300s", path, resp.Status, raw)
	}
	return json.Unmarshal(raw, out)
}

// OpenAIEmbedder uses text-embedding-3-small (1536d, matching the DB column).
type OpenAIEmbedder struct {
	cfg    openaiConfig
	dim    int
	batch  int
}

// NewOpenAIEmbedder builds the embedder; nil when unconfigured.
func NewOpenAIEmbedder() *OpenAIEmbedder {
	cfg := newOpenAIConfig(envOr("OPENAI_EMBED_MODEL", "text-embedding-3-small"))
	if !cfg.enabled() {
		return nil
	}
	return &OpenAIEmbedder{cfg: cfg, dim: 1536, batch: 64}
}

func (e *OpenAIEmbedder) Dim() int     { return e.dim }
func (e *OpenAIEmbedder) Name() string { return "openai-" + e.cfg.model }

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.batch {
		end := min(start+e.batch, len(texts))
		var resp struct {
			Data []struct {
				Index     int       `json:"index"`
				Embedding []float32 `json:"embedding"`
			} `json:"data"`
		}
		err := e.cfg.post(ctx, "/embeddings", map[string]any{
			"model": e.cfg.model,
			"input": texts[start:end],
		}, &resp)
		if err != nil {
			return nil, err
		}
		byIdx := make(map[int][]float32, len(resp.Data))
		for _, d := range resp.Data {
			byIdx[d.Index] = d.Embedding
		}
		for i := range texts[start:end] {
			vec, ok := byIdx[i]
			if !ok {
				return nil, fmt.Errorf("openai embeddings: missing index %d", i)
			}
			out = append(out, vec)
		}
	}
	return out, nil
}

// OpenAISummarizer condenses transcripts with a chat model.
type OpenAISummarizer struct {
	cfg openaiConfig
}

// NewOpenAISummarizer builds the summarizer; nil when unconfigured.
func NewOpenAISummarizer() *OpenAISummarizer {
	cfg := newOpenAIConfig(envOr("OPENAI_SUMMARY_MODEL", "gpt-4o-mini"))
	if !cfg.enabled() {
		return nil
	}
	return &OpenAISummarizer{cfg: cfg}
}

func (s *OpenAISummarizer) Name() string { return "openai-" + s.cfg.model }

func (s *OpenAISummarizer) Summarize(ctx context.Context, transcript string) (string, error) {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	err := s.cfg.post(ctx, "/chat/completions", map[string]any{
		"model": s.cfg.model,
		"messages": []map[string]string{
			{"role": "system", "content": "Summarize this audio episode in 2-3 sentences for a radio platform listing. No preamble."},
			{"role": "user", "content": truncate(transcript, 24000)},
		},
		"temperature": 0.3,
	}, &resp)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("openai summarize: no choices")
	}
	return resp.Choices[0].Message.Content, nil
}

// OpenAITranscriber uses whisper-1 over the audio API.
type OpenAITranscriber struct {
	cfg openaiConfig
}

// NewOpenAITranscriber builds the transcriber; nil when unconfigured.
func NewOpenAITranscriber() *OpenAITranscriber {
	cfg := newOpenAIConfig(envOr("OPENAI_STT_MODEL", "whisper-1"))
	if !cfg.enabled() {
		return nil
	}
	return &OpenAITranscriber{cfg: cfg}
}

func (t *OpenAITranscriber) Name() string { return "openai-" + t.cfg.model }

// Transcribe posts the audio object with verbose_json to get timestamps.
func (t *OpenAITranscriber) Transcribe(ctx context.Context, storageKey string) ([]Segment, error) {
	f, err := os.Open(storageKey)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Standard multipart/form-data with the file inline.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filepath.Base(storageKey))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return nil, err
	}
	if err := mw.WriteField("model", t.cfg.model); err != nil {
		return nil, err
	}
	if err := mw.WriteField("response_format", "verbose_json"); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		t.cfg.base+"/audio/transcriptions", &buf)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+t.cfg.apiKey)
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := t.cfg.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<24))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai transcribe: %s: %.300s", resp.Status, raw)
	}

	var doc struct {
		Segments []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("transcription output: %w", err)
	}
	segs := make([]Segment, 0, len(doc.Segments))
	for _, s := range doc.Segments {
		text := trimSpace(s.Text)
		if text == "" {
			continue
		}
		segs = append(segs, Segment{
			StartMs: int(s.Start * 1000),
			EndMs:   int(s.End * 1000),
			Text:    text,
		})
	}
	return segs, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
