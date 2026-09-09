// Command worker runs Goonj background jobs. The audio pipeline downloads
// the uploaded original from object storage, validates it with ffprobe,
// loudness-normalizes, transcodes three quality variants, extracts waveform
// peaks and duration, uploads results, and marks the audio READY.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/storage"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/shared/tasks"
)

// Quality presets for transcoding (mono-ish speech/music balance).
type qualityPreset struct {
	name    string
	bitrate string
	mime    string
}

var presets = []qualityPreset{
	{"low", "64k", "audio/mpeg"},
	{"medium", "128k", "audio/mpeg"},
	{"high", "256k", "audio/mpeg"},
}

func main() {
	if err := run(); err != nil {
		slog.Error("worker exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.Load("goonj-worker")
	logger := platform.NewLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := platform.NewPostgres(ctx, cfg, logger)
	if err != nil {
		return err
	}
	objstore, err := storage.New(ctx, storage.Config{
		InternalEndpoint: cfg.S3InternalEndpoint,
		PublicEndpoint:   cfg.S3PublicEndpoint,
		Region:           cfg.S3Region,
		Bucket:           cfg.S3Bucket,
		AccessKeyID:      cfg.S3AccessKeyID,
		SecretAccessKey:  cfg.S3SecretAccessKey,
		UsePathStyle:     cfg.S3UsePathStyle,
	})
	if err != nil {
		return err
	}

	redisOpt, err := asynq.ParseRedisURI(cfg.RedisURL)
	if err != nil {
		return err
	}
	srv := asynq.NewServer(redisOpt, asynq.Config{Concurrency: 2})

	store := NewResultStore(pool)
	handler := newAudioHandler(objstore, store, logger)

	mux := asynq.NewServeMux()
	mux.HandleFunc(tasks.TypeProcessAudio, handler.processAudio)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("worker starting")
		errCh <- srv.Run(mux)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		srv.Shutdown()
		return nil
	}
}

// audioHandler processes uploaded audio through FFmpeg.
type audioHandler struct {
	store  *storage.ObjectStore
	results *ResultStore
	log    *slog.Logger
}

func newAudioHandler(store *storage.ObjectStore, results *ResultStore, log *slog.Logger) *audioHandler {
	return &audioHandler{store: store, results: results, log: log}
}

func (h *audioHandler) processAudio(ctx context.Context, t *asynq.Task) error {
	payload, err := tasks.DecodeProcessAudio(t)
	if err != nil {
		return fmt.Errorf("decode payload: %w", err) // no retry on bad payload
	}
	h.log.Info("processing audio", slog.String("audio", payload.AudioID))

	workDir, err := os.MkdirTemp("", "goonj-audio-")
	if err != nil {
		return fmt.Errorf("create workdir: %w", err)
	}
	defer os.RemoveAll(workDir)

	// 1. Download the original.
	originalPath := workDir + "/original"
	if err := h.download(ctx, payload.StorageKey, originalPath); err != nil {
		return h.fail(ctx, payload.AudioID, fmt.Errorf("download original: %w", err))
	}

	// 2. Validate with ffprobe (never trust client metadata).
	durationMs, err := probeDurationMs(originalPath)
	if err != nil {
		return h.fail(ctx, payload.AudioID, fmt.Errorf("invalid audio: %w", err))
	}
	if durationMs <= 0 || durationMs > 12*60*60*1000 {
		return h.fail(ctx, payload.AudioID, fmt.Errorf("duration out of range: %dms", durationMs))
	}

	// 3. Loudness-normalize once into a working WAV.
	normalizedWav := workDir + "/normalized.wav"
	if err := runFFmpeg(ctx, "-y", "-i", originalPath,
		"-af", "loudnorm=I=-16:TP=-1.5:LRA=11",
		"-ar", "44100", "-ac", "2",
		"-f", "wav", normalizedWav); err != nil {
		return h.fail(ctx, payload.AudioID, fmt.Errorf("loudnorm: %w", err))
	}

	// 4. Transcode quality variants.
	totalSize := int64(0)
	for _, p := range presets {
		outPath := fmt.Sprintf("%s/%s.mp3", workDir, p.name)
		if err := runFFmpeg(ctx, "-y", "-i", normalizedWav,
			"-codec:a", "libmp3lame", "-b:a", p.bitrate,
			"-f", "mp3", outPath); err != nil {
			return h.fail(ctx, payload.AudioID, fmt.Errorf("transcode %s: %w", p.name, err))
		}
		key := fmt.Sprintf("audio/processed/%s/%s.mp3", payload.AudioID, p.name)
		f, err := os.Open(outPath)
		if err != nil {
			return h.fail(ctx, payload.AudioID, err)
		}
		size, _ := f.Seek(0, 2)
		_, _ = f.Seek(0, 0)
		if err := h.store.Put(ctx, key, p.mime, f); err != nil {
			f.Close()
			return h.fail(ctx, payload.AudioID, fmt.Errorf("upload %s: %w", p.name, err))
		}
		f.Close()
		totalSize += size

		bitrate := 0
		fmt.Sscanf(p.bitrate, "%dk", &bitrate)
		if err := h.results.AddFile(ctx, payload.AudioID, QualityFile{
			Quality: p.name, StorageKey: key, BitrateKbps: bitrate,
			SizeBytes: size, MimeType: p.mime,
		}); err != nil {
			return h.fail(ctx, payload.AudioID, err)
		}
	}

	// 5. Waveform peaks (use the low-quality mp3 for speed).
	peaks, err := waveformPeaks(ctx, fmt.Sprintf("%s/low.mp3", workDir), 400)
	if err != nil {
		h.log.Warn("waveform generation failed; continuing", slog.Any("error", err))
		peaks = []int{}
	}

	// 6. Mark READY with metadata.
	waveJSON, _ := json.Marshal(peaks)
	if err := h.results.MarkReady(ctx, payload.AudioID, durationMs, waveJSON, totalSize, "audio/mpeg"); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}

	h.log.Info("audio ready",
		slog.String("audio", payload.AudioID),
		slog.Int("duration_ms", durationMs),
		slog.Int("peaks", len(peaks)))
	return nil
}

// fail marks the audio FAILED; unexpected infra errors are returned for retry.
func (h *audioHandler) fail(ctx context.Context, audioID string, cause error) error {
	h.log.Error("audio processing failed",
		slog.String("audio", audioID), slog.Any("error", cause))
	if err := h.results.SetStatus(ctx, audioID, "FAILED"); err != nil {
		h.log.Error("could not mark FAILED", slog.String("audio", audioID), slog.Any("error", err))
	}
	// Validation failures are permanent; infra failures may retry. Simplify:
	// return the error so Asynq retries a few times before the DLQ.
	return cause
}

func (h *audioHandler) download(ctx context.Context, key, dest string) error {
	rc, err := h.store.Get(ctx, key)
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(rc)
	return err
}

// runFFmpeg runs an ffmpeg command, capturing stderr for diagnostics.
func runFFmpeg(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-hide_banner", "-loglevel", "error"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg %v: %s: %w", args[:min(3, len(args))], tail(string(out), 200), err)
	}
	return nil
}

// probeDurationMs extracts duration in milliseconds via ffprobe.
func probeDurationMs(path string) (int, error) {
	out, err := exec.Command("ffprobe",
		"-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	var seconds float64
	if _, err := fmt.Sscanf(string(out), "%f", &seconds); err != nil {
		return 0, fmt.Errorf("parse duration %q: %w", string(out), err)
	}
	return int(seconds * 1000), nil
}

// waveformPeaks extracts N normalized peak amplitudes (0-100) from audio.
func waveformPeaks(ctx context.Context, path string, n int) ([]int, error) {
	raw, err := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-loglevel", "error",
		"-i", path,
		"-ac", "1", "-ar", "8000", "-f", "s16le", "-").Output()
	if err != nil {
		return nil, err
	}
	samples := len(raw) / 2
	if samples == 0 {
		return nil, fmt.Errorf("no samples")
	}
	bucket := samples / n
	if bucket == 0 {
		bucket = 1
	}
	peaks := make([]int, 0, n)
	for i := 0; i < n; i++ {
		start := i * bucket
		if start+bucket > samples {
			break
		}
		var max int16
		for j := start; j < start+bucket; j++ {
			v := int16(uint16(raw[j*2]) | uint16(raw[j*2+1])<<8)
			if v > max {
				max = v
			}
		}
		amp := int(float64(max) / 32767 * 100)
		if amp < 0 {
			amp = -amp
		}
		peaks = append(peaks, amp)
	}
	return peaks, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = time.Now
