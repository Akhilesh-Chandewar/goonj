// Package livekit provides the server-side LiveKit client: room-composite
// egress recording of live audio rooms. It sits behind the live module's
// Recorder interface so the provider can be swapped (LiveKit Cloud, self
// -hosted v2) without touching business logic.
package livekit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/livekit/protocol/livekit"
	"github.com/twitchtv/twirp"
)

// Egress records a LiveKit room to object storage via room-composite egress.
// The LiveKit server exposes its RPC APIs over Twirp (JSON/protobuf HTTP) on
// the same port as the WS endpoint, so no extra dependency is required.
type Egress struct {
	egress    livekit.Egress
	room      livekit.RoomService
	outputCfg OutputConfig
}

// OutputConfig tells egress where to write the recording. Credentials are
// passed to the egress service per request (never to clients).
type OutputConfig struct {
	// Endpoint of the S3-compatible storage as reachable by the egress
	// container (e.g. http://floci:4566 in compose).
	Endpoint string
	Region   string
	Bucket   string
	// Object prefix for recordings, e.g. "live/recordings".
	Prefix string

	AccessKeyID     string
	SecretAccessKey string
	// UsePathStyle is required for floci/MinIO.
	UsePathStyle bool
}

// NewEgress builds the egress client. apiURL is the LiveKit HTTP endpoint
// (ws(s)://host:port works: the scheme is normalized), and auth uses the same
// key/secret pair as token minting.
func NewEgress(apiURL string, apiKey, apiSecret string, out OutputConfig) *Egress {
	client := newAuthenticatingClient(apiKey, apiSecret)
	base := normalizeHTTPBase(apiURL)
	return &Egress{
		egress:    livekit.NewEgressJSONClient(base, client),
		room:      livekit.NewRoomServiceJSONClient(base, client),
		outputCfg: out,
	}
}

// normalizeHTTPBase converts a LiveKit ws URL to the Twirp HTTP base.
func normalizeHTTPBase(u string) string {
	switch {
	case len(u) >= 4 && u[:4] == "wss:":
		return "https://" + u[6:]
	case len(u) >= 3 && u[:3] == "ws:":
		return "http://" + u[5:]
	default:
		return u
	}
}

// RecordingOutcome summarizes one recording attempt.
type RecordingOutcome struct {
	EgressID string
	Status   livekit.EgressStatus
	// FileSize in bytes (0 when unknown).
	FileSize int64
	// FileLocation is the storage path of the recording (relative to the
	// configured bucket/prefix, e.g. "live/recordings/xyz.mp3").
	FileLocation string
	Error        string
}

// StartTrackPCM begins track egress streaming the source track's raw PCM
// into the given websocket URL (the translator's PCMPump). Per LiveKit docs,
// websocket output is audio-only, pcm_s16le at the track's sample rate —
// exactly what the OpenAI Realtime translation session consumes. The
// connection closes when the track is unpublished or the speaker leaves.
func (e *Egress) StartTrackPCM(ctx context.Context, roomName, trackID, wsURL string) (egressID string, err error) {
	info, err := e.egress.StartTrackEgress(ctx, &livekit.TrackEgressRequest{
		RoomName: roomName,
		TrackId:  trackID,
		Output:   &livekit.TrackEgressRequest_WebsocketUrl{WebsocketUrl: wsURL},
	})
	if err != nil {
		return "", fmt.Errorf("start track egress: %w", err)
	}
	return info.EgressId, nil
}

// ListParticipants returns the participants currently in a room (used to
// locate the creator's published microphone track for the translator's
// track egress).
func (e *Egress) ListParticipants(ctx context.Context, roomName string) ([]*livekit.ParticipantInfo, error) {
	res, err := e.room.ListParticipants(ctx, &livekit.ListParticipantsRequest{Room: roomName})
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	return res.Participants, nil
}

// SourceMicrophoneTrack finds the first published microphone track in a room
// (the creator's, in the single-publisher model Goonj uses).
func (e *Egress) SourceMicrophoneTrack(ctx context.Context, roomName string) (trackID string, err error) {
	participants, err := e.ListParticipants(ctx, roomName)
	if err != nil {
		return "", err
	}
	for _, p := range participants {
		for _, tr := range p.Tracks {
			if tr.Type == livekit.TrackType_AUDIO && tr.Source == livekit.TrackSource_MICROPHONE {
				return tr.Sid, nil
			}
		}
	}
	return "", ErrEgressNotFound
}

// StartRecording begins room-composite egress writing an MP3 to the
// configured bucket under <prefix>/<roomName>-<time>.mp3. The room is created
// explicitly first: egress cannot start on a room that does not exist yet
// (LiveKit otherwise auto-creates rooms on first participant join).
func (e *Egress) StartRecording(ctx context.Context, roomName string) (*RecordingOutcome, error) {
	if _, err := e.room.CreateRoom(ctx, &livekit.CreateRoomRequest{
		Name:         roomName,
		EmptyTimeout: 300, // seconds; egress participant keeps the room alive
	}); err != nil {
		var terr twirp.Error
		if !(errors.As(err, &terr) && terr.Code() == twirp.AlreadyExists) {
			return nil, fmt.Errorf("create room for egress: %w", err)
		}
	}
	info, err := e.egress.StartRoomCompositeEgress(ctx, &livekit.RoomCompositeEgressRequest{
		RoomName:  roomName,
		AudioOnly: true,
		Layout:    "speaker", // audio-only ignores layout; keeps the web SDK happy
		FileOutputs: []*livekit.EncodedFileOutput{{
			FileType:        livekit.EncodedFileType_MP3,
			DisableManifest: true,
			Filepath:        e.recordingFilepath(),
			Output: &livekit.EncodedFileOutput_S3{
				S3: &livekit.S3Upload{
					AccessKey:      e.outputCfg.AccessKeyID,
					Secret:         e.outputCfg.SecretAccessKey,
					Region:         e.outputCfg.Region,
					Endpoint:       e.outputCfg.Endpoint,
					Bucket:         e.outputCfg.Bucket,
					ForcePathStyle: e.outputCfg.UsePathStyle,
				},
			},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("start room composite egress: %w", err)
	}
	return &RecordingOutcome{EgressID: info.EgressId, Status: info.Status}, nil
}

// recordingFilepath returns the egress filepath template for recordings
// ("<prefix>/{room_name}-{time}.mp3"). Templates are expanded by egress.
func (e *Egress) recordingFilepath() string {
	if e.outputCfg.Prefix == "" {
		return "{room_name}-{time}.mp3"
	}
	return e.outputCfg.Prefix + "/{room_name}-{time}.mp3"
}

// StopRecording requests a graceful stop; LiveKit finalizes and uploads the
// file. Poll RecordingStatus until COMPLETE.
func (e *Egress) StopRecording(ctx context.Context, egressID string) (*RecordingOutcome, error) {
	info, err := e.egress.StopEgress(ctx, &livekit.StopEgressRequest{EgressId: egressID})
	if err != nil {
		return nil, fmt.Errorf("stop egress: %w", err)
	}
	return outcomeFromInfo(info), nil
}

// RecordingStatus polls the egress state and result metadata.
func (e *Egress) RecordingStatus(ctx context.Context, egressID string) (*RecordingOutcome, error) {
	res, err := e.egress.ListEgress(ctx, &livekit.ListEgressRequest{EgressId: egressID})
	if err != nil {
		return nil, fmt.Errorf("get egress: %w", err)
	}
	if len(res.Items) == 0 {
		return nil, ErrEgressNotFound
	}
	return outcomeFromInfo(res.Items[0]), nil
}

// CloseRoom deletes a room and disconnects all participants (stream end).
func (e *Egress) CloseRoom(ctx context.Context, roomName string) error {
	_, err := e.room.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: roomName})
	if err != nil {
		// A missing room is fine: it auto-deletes when empty.
		var terr twirp.Error
		if errors.As(err, &terr) && terr.Code() == twirp.NotFound {
			return nil
		}
		return fmt.Errorf("delete room: %w", err)
	}
	return nil
}

// ErrEgressNotFound is returned when an egress id is unknown to LiveKit.
var ErrEgressNotFound = fmt.Errorf("livekit: egress not found")

func outcomeFromInfo(info *livekit.EgressInfo) *RecordingOutcome {
	out := &RecordingOutcome{
		EgressID: info.EgressId,
		Status:   info.Status,
		Error:    info.Error,
	}
	if f := info.GetFileResults(); len(f) > 0 {
		out.FileSize = f[0].GetSize()
		out.FileLocation = f[0].GetLocation()
	} else if req := info.GetRoomComposite(); req != nil {
		if fo := req.GetFile(); fo != nil {
			out.FileLocation = fo.GetFilepath()
		}
	}
	return out
}

// WaitComplete polls until the egress completes or fails. It returns the
// final outcome. Poll interval grows (1s → 5s cap) to keep load low. Unknown
// egress ids (eventual consistency) are retried until the deadline.
func (e *Egress) WaitComplete(ctx context.Context, egressID string, timeout time.Duration) (*RecordingOutcome, error) {
	deadline := time.Now().Add(timeout)
	delay := time.Second
	for {
		out, err := e.RecordingStatus(ctx, egressID)
		if err != nil && !errors.Is(err, ErrEgressNotFound) {
			return nil, err // hard RPC failure: caller retries with backoff
		}
		if err == nil {
			switch out.Status {
			case livekit.EgressStatus_EGRESS_COMPLETE:
				return out, nil
			case livekit.EgressStatus_EGRESS_FAILED,
				livekit.EgressStatus_EGRESS_ABORTED,
				livekit.EgressStatus_EGRESS_LIMIT_REACHED:
				return out, fmt.Errorf("egress %s failed: %s", out.EgressID, out.Error)
			}
		}
		if time.Now().After(deadline) {
			return out, fmt.Errorf("egress %s not complete after %s", egressID, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			delay = minDuration(delay*2, 5*time.Second)
		}
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

// newAuthenticatingClient wraps an *http.Client to inject LiveKit's
// bearer-token auth header (JWT signed with the API key/secret) on every RPC.
func newAuthenticatingClient(apiKey, apiSecret string) *http.Client {
	return &http.Client{Transport: &authTransport{apiKey: apiKey, apiSecret: apiSecret}}
}

type authTransport struct {
	apiKey    string
	apiSecret string
	base      http.RoundTripper
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	// 2-minute validity is plenty for a single RPC.
	token, err := serverToken(t.apiKey, t.apiSecret)
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+token)
	return base.RoundTrip(clone)
}

// serverToken mints a non-participant JWT for server RPCs.
func serverToken(apiKey, apiSecret string) (string, error) {
	return serverAccessToken(apiKey, apiSecret)
}
