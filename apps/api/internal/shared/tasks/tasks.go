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
