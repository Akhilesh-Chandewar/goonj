package main

import (
	"context"

	"github.com/hibiken/asynq"

	livekitinfra "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/livekit"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/shared/tasks"
)

// egressRecorder adapts the infrastructure egress client to the live module's
// Recorder interface, translating the generic outcome into the narrow tuple
// the module needs.
type egressRecorder struct {
	egress *livekitinfra.Egress
}

// StartRecording begins audio-only room-composite egress for the room. The
// egress service renders the room itself, so no browser template is required
// and the recording survives the creator disconnecting.
func (e egressRecorder) StartRecording(ctx context.Context, roomName string) (string, error) {
	out, err := e.egress.StartRecording(ctx, roomName)
	if err != nil {
		return "", err
	}
	return out.EgressID, nil
}

func (e egressRecorder) StopRecording(ctx context.Context, egressID string) error {
	_, err := e.egress.StopRecording(ctx, egressID)
	return err
}

func (e egressRecorder) RecordingStatus(ctx context.Context, egressID string) (string, int64, string, error) {
	out, err := e.egress.RecordingStatus(ctx, egressID)
	if err != nil {
		return "", 0, "", err
	}
	return out.Status.String(), out.FileSize, out.FileLocation, nil
}

// recordingFinalizer implements live.RecordingFinalizer by enqueueing the
// worker's live:finalize-recording task.
type recordingFinalizer struct {
	queue *asynq.Client
}

func (f recordingFinalizer) EnqueueRecordingFinalize(ctx context.Context, sessionID, egressID string) error {
	task, err := tasks.NewFinalizeRecording(tasks.FinalizeRecordingPayload{
		SessionID: sessionID,
		EgressID:  egressID,
	})
	if err != nil {
		return err
	}
	_, err = f.queue.EnqueueContext(ctx, task)
	return err
}
