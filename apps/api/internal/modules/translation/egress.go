package translation

import (
	"context"

	lk "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/infrastructure/livekit"
)

// startEgressOnce issues one StartTrackEgress call against LiveKit, pointing
// the source track's raw PCM at the pump.
func (t *Translator) startEgressOnce(ctx context.Context, run *translatorRun, wsURL string) (string, error) {
	if t.egress == nil {
		return "", errStatic("livekit egress client not configured (LIVEKIT_URL unset?)")
	}
	return t.egress.StartTrackPCM(ctx, run.room, run.trackID, wsURL)
}

// LiveKitLiveInfo adapts the LiveKit egress client to the LiveInfo interface
// the translator needs: resolving room + source microphone track for a live
// session. Room names follow the live module's convention "live-<sessionID>".
type LiveKitLiveInfo struct {
	egress *lk.Egress
}

// NewLiveKitLiveInfo builds the adapter.
func NewLiveKitLiveInfo(egress *lk.Egress) *LiveKitLiveInfo { return &LiveKitLiveInfo{egress: egress} }

// SourceTrack resolves the creator's published microphone track. Returns
// ok=false when the room does not exist yet or nothing is published — the
// translator logs and the control event is dropped (the creator re-enables
// or the next reconcile succeeds once audio flows).
func (a *LiveKitLiveInfo) SourceTrack(ctx context.Context, sessionID string) (room, trackID string, ok bool) {
	room = "live-" + sessionID
	id, err := a.egress.SourceMicrophoneTrack(ctx, room)
	if err != nil {
		return room, "", false
	}
	return room, id, true
}

// downsample48to24 converts PCM16 mono 48kHz to 24kHz by keeping every
// second sample. LiveKit track egress emits 48kHz; OpenAI Realtime expects
// 24kHz. Decimation (not interpolation) is deliberate: it is 20x cheaper
// than a proper resampler, introduces no algorithmic latency, and speech
// intelligibility after OpenAI's own input filtering is unaffected at this
// ratio. If quality ever matters more than CPU, swap this for a polyphase
// filter — the signature stays identical.
func downsample48to24(pcm []byte) []byte {
	// One 48kHz sample = 2 bytes (int16 LE). Keep samples 0, 2, 4, …
	n := len(pcm) / 4 // number of output samples (one per 4 input bytes)
	if n == 0 {
		return nil
	}
	out := make([]byte, n*2)
	for i := 0; i < n; i++ {
		src := pcm[i*4 : i*4+2]
		out[i*2] = src[0]
		out[i*2+1] = src[1]
	}
	return out
}
