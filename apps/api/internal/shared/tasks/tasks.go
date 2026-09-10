// Package tasks defines the Asynq task types and payloads shared by the API
// (producer) and workers (consumers). Payloads are simple JSON so retries
// stay idempotent.
package tasks

import (
	"encoding/json"

	"github.com/hibiken/asynq"
)

// Task type names.
const (
	TypeProcessAudio = "audio:process"
	// TypeFinalizeRecording converts a finished live recording into a draft
	// episode: polls egress completion, creates the audio row, and hands the
	// file to the regular audio pipeline.
	TypeFinalizeRecording = "live:finalize-recording"
)

// ProcessAudioPayload instructs the worker to run the FFmpeg pipeline on an
// uploaded original.
type ProcessAudioPayload struct {
	AudioID string `json:"audio_id"`
	// StorageKey of the uploaded original object.
	StorageKey string `json:"storage_key"`
	// OriginalMime as reported by the uploader (validated again server-side).
	OriginalMime string `json:"original_mime,omitempty"`
}

// NewProcessAudio builds a process-audio task.
func NewProcessAudio(p ProcessAudioPayload) (*asynq.Task, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeProcessAudio, body), nil
}

// DecodeProcessAudio parses a process-audio task payload.
func DecodeProcessAudio(t *asynq.Task) (ProcessAudioPayload, error) {
	var p ProcessAudioPayload
	err := json.Unmarshal(t.Payload(), &p)
	return p, err
}

// FinalizeRecordingPayload instructs the worker to turn a stopped egress
// recording into a draft audio episode for the session.
// Retries are idempotent: the worker checks live_recordings.status first.
type FinalizeRecordingPayload struct {
	SessionID string `json:"session_id"`
	// EgressID identifies the live_recordings row and the LiveKit egress.
	EgressID string `json:"egress_id"`
}

// NewFinalizeRecording builds a finalize-recording task.
func NewFinalizeRecording(p FinalizeRecordingPayload) (*asynq.Task, error) {
	body, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeFinalizeRecording, body), nil
}

// DecodeFinalizeRecording parses a finalize-recording task payload.
func DecodeFinalizeRecording(t *asynq.Task) (FinalizeRecordingPayload, error) {
	var p FinalizeRecordingPayload
	err := json.Unmarshal(t.Payload(), &p)
	return p, err
}
