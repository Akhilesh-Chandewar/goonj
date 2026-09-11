package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalTranscriber shells out to a whisper.cpp `main` binary when
// WHISPER_CPP_BIN + WHISPER_CPP_MODEL are configured; otherwise it reports
// ErrUnavailable so the worker can skip transcription gracefully (the
// pipeline stays green; semantic search just has nothing to embed yet).
// Vendor-backed transcription (OpenAI) plugs in behind the same interface.
type LocalTranscriber struct {
	binary string
	model  string
}

// ErrUnavailable means no usable transcriber is configured.
var ErrUnavailable = errors.New("transcription unavailable: configure WHISPER_CPP_BIN and WHISPER_CPP_MODEL (or an OpenAI key)")

// NewLocalTranscriber reads its config from the environment.
func NewLocalTranscriber() *LocalTranscriber {
	return &LocalTranscriber{
		binary: os.Getenv("WHISPER_CPP_BIN"),
		model:  os.Getenv("WHISPER_CPP_MODEL"),
	}
}

func (t *LocalTranscriber) Name() string { return "whisper.cpp" }

// Unconfigured reports whether the binary/model env vars are unset, letting
// callers skip transcription work without invoking the binary.
func (t *LocalTranscriber) Unconfigured() bool { return t.binary == "" || t.model == "" }

// Transcribe downloads the object to a temp file and runs whisper.cpp with
// JSON output, mapping segments into timed Segments.
func (t *LocalTranscriber) Transcribe(ctx context.Context, storageKey string) ([]Segment, error) {
	if t.binary == "" || t.model == "" {
		return nil, ErrUnavailable
	}

	tmp, err := os.MkdirTemp("", "goonj-stt-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	wav := filepath.Join(tmp, "input.wav")

	// whisper.cpp requires 16kHz mono WAV; ffmpeg is already a worker dep.
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-y", "-i", storageKey,
		"-ar", "16000", "-ac", "1", wav).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ffmpeg prep: %w: %s", err, tail(out))
	}

	out, err := exec.CommandContext(ctx, t.binary,
		"-m", t.model, "-f", wav, "-oj", "-of", filepath.Join(tmp, "out"),
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("whisper.cpp: %w: %s", err, tail(out))
	}

	return parseWhisperJSON(filepath.Join(tmp, "out.json"))
}

// parseWhisperJSON maps whisper.cpp's segment list.
func parseWhisperJSON(path string) ([]Segment, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Transcription []struct {
			Timestamps struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"timestamps"`
			Text string `json:"text"`
		} `json:"transcription"`
	}
	// encoding/json ignores unknown fields by default; whisper.cpp emits
	// version-dependent metadata we don't model.
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("whisper output: %w", err)
	}
	segs := make([]Segment, 0, len(doc.Transcription))
	for _, s := range doc.Transcription {
		text := strings.TrimSpace(s.Text)
		if text == "" || text == "[BLANK_AUDIO]" {
			continue
		}
		segs = append(segs, Segment{
			StartMs: parseWhisperClock(s.Timestamps.From),
			EndMs:   parseWhisperClock(s.Timestamps.To),
			Text:    text,
		})
	}
	return segs, nil
}

// parseWhisperClock converts "00:00:03,250" to milliseconds.
func parseWhisperClock(s string) int {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0
	}
	secParts := strings.Split(parts[2], ",")
	sec, _ := strconv.Atoi(secParts[0])
	ms := 0
	if len(secParts) > 1 {
		ms, _ = strconv.Atoi(secParts[1])
	}
	min, _ := strconv.Atoi(parts[1])
	hr, _ := strconv.Atoi(parts[0])
	return ((hr*60+min)*60+sec)*1000 + ms
}

func tail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[len(s)-300:]
	}
	return s
}
