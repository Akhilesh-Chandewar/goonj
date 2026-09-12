package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Service implements the live session lifecycle. It depends on interfaces
// (Streamer, Recorder) and thin adapters (Store, Presence) so providers can be
// swapped without touching business logic.
type Service struct {
	store      *Store
	recordings *RecordingStore
	presence   *Presence
	streamer   Streamer
	recorder   Recorder // nil when egress is unavailable
	enqueuer   RecordingFinalizer
	notifier   LiveNotifier // optional "went live" fan-out
	log        *slog.Logger
}

// RecordingFinalizer enqueues the worker job that turns a finished recording
// into a draft episode (asynq producer interface; eases testing).
type RecordingFinalizer interface {
	EnqueueRecordingFinalize(ctx context.Context, sessionID, egressID string) error
}

func NewService(store *Store, recordings *RecordingStore, presence *Presence, streamer Streamer, recorder Recorder, finalizer RecordingFinalizer, log *slog.Logger) *Service {
	return &Service{
		store:      store,
		recordings: recordings,
		presence:   presence,
		streamer:   streamer,
		recorder:   recorder,
		enqueuer:   finalizer,
		log:        log,
	}
}

var (
	ErrNotCreator      = errors.New("creator profile required")
	ErrNotOwner        = errors.New("not the session owner")
	ErrInvalidStatus   = errors.New("invalid session state for this action")
	ErrInvalidTitle    = errors.New("title must be 1-200 characters")
	ErrPrivateSession  = errors.New("session is private")
	ErrSessionNotFound = ErrNotFound
)

// CreateInput is the go-live creation payload.
type CreateInput struct {
	UserID      string
	Title       string
	Description string
	Category    string
	Visibility  string
}

// ErrRecordingUnavailable is returned when egress is not configured; sessions
// still work, they just do not produce recordings.
var ErrRecordingUnavailable = errors.New("recording is temporarily unavailable")

// Create makes a new SCHEDULED session for the caller's creator profile.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Session, error) {
	if len(in.Title) == 0 || len(in.Title) > 200 {
		return nil, ErrInvalidTitle
	}
	if in.Visibility == "" {
		in.Visibility = "public"
	}
	if in.Category == "" {
		in.Category = "general"
	}

	creatorID, err := s.store.CreatorIDByUserID(ctx, in.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotCreator
		}
		return nil, err
	}

	session, err := s.store.Create(ctx, creatorID, in.Title, in.Description, in.Category, in.Visibility)
	if err != nil {
		return nil, err
	}

	s.store.AppendEvent(ctx, session.ID, "session.created", []byte(`{}`))
	s.log.Info("live session created",
		slog.String("session", session.ID), slog.String("creator", creatorID))
	return session, nil
}

// Start moves a SCHEDULED session to LIVE and mints the creator's publish
// token. The creator's client connects to LiveKit with it and starts
// publishing their microphone track.
func (s *Service) Start(ctx context.Context, sessionID, userID, username string) (*Session, *StreamToken, error) {
	session, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if session.CreatorID != s.mustCreatorID(ctx, userID) {
		return nil, nil, ErrNotOwner
	}
	if session.Status == StatusLive {
		return nil, nil, ErrInvalidStatus
	}
	if session.Status != StatusScheduled {
		return nil, nil, ErrInvalidStatus
	}

	roomName := "live-" + session.ID
	if err := s.streamer.EnsureRoom(ctx, roomName); err != nil {
		return nil, nil, err
	} // Recording starts explicitly via StartRecordingNow once the creator's
	// client is connected and publishing (participant egress needs a live
	// track to record).
	token, err := s.streamer.JoinToken(ctx, roomName, "creator-"+userID, StreamGrant{Publisher: true}, 6*time.Hour)
	if err != nil {
		return nil, nil, err
	}

	if err := s.store.MarkStarted(ctx, sessionID); err != nil {
		return nil, nil, err
	}
	if err := s.store.UpsertStream(ctx, &Stream{
		SessionID:    sessionID,
		RoomName:     roomName,
		IngestToken:  token.Token,
		TokenExpires: token.ExpiresAt,
	}); err != nil {
		return nil, nil, err
	}
	_ = s.store.SetCreatorLive(ctx, session.CreatorID, true)
	s.store.AppendEvent(ctx, sessionID, "session.started", []byte(`{}`))

	// Auto-start recording (PROBLEMS #7): the client-driven recording start
	// missed the first seconds of the show. Room-composite egress does not
	// need a published track, so starting here captures everything. The
	// creator's StartRecordingNow remains as an idempotent fallback.
	if s.recorder != nil {
		if _, err := s.StartRecordingNow(ctx, sessionID, userID); err != nil {
			s.log.Warn("auto-record failed; client fallback remains",
				slog.String("session", sessionID), slog.Any("error", err))
		}
	}

	// Fan out "went live" to followers (best-effort, off the hot path).
	if s.notifier != nil {
		sid, title := session.ID, session.Title
		creatorID := session.CreatorID
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.notifier.NotifyLiveStarted(bg, creatorID, session.CreatorName, sid, title)
		}()
	}

	fresh, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	return fresh, token, nil
}

// Join adds a listener to a LIVE public session and returns their subscribe
// token plus current listener counts.
func (s *Service) Join(ctx context.Context, sessionID, userID, username string) (*Session, *StreamToken, int, int64, error) {
	session, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, nil, 0, 0, err
	}
	if session.Status != StatusLive {
		return nil, nil, 0, 0, ErrInvalidStatus
	}
	if session.Visibility != "public" {
		// Phase 2: only public sessions; private invites land later.
		return nil, nil, 0, 0, ErrPrivateSession
	}

	roomName := "live-" + session.ID
	token, err := s.streamer.JoinToken(ctx, roomName, "listener-"+userID, StreamGrant{Publisher: false}, 6*time.Hour)
	if err != nil {
		return nil, nil, 0, 0, err
	}

	conc, uniq, err := s.presence.Join(ctx, sessionID, userID)
	if err != nil {
		return nil, nil, 0, 0, err
	}

	// Fan out presence to the room.
	s.broadcast(ctx, sessionID, map[string]any{
		"type":       "listener_joined",
		"user_id":    userID,
		"username":   username,
		"concurrent": conc,
		"unique":     uniq,
	})

	return session, token, conc, uniq, nil
}

// Heartbeat keeps a listener marked present.
func (s *Service) Heartbeat(ctx context.Context, sessionID, userID string) error {
	return s.presence.Heartbeat(ctx, sessionID, userID)
}

// Leave removes a listener and fans out updated counts.
func (s *Service) Leave(ctx context.Context, sessionID, userID, username string) error {
	conc, err := s.presence.Leave(ctx, sessionID, userID)
	if err != nil {
		return err
	}
	s.broadcast(ctx, sessionID, map[string]any{
		"type":       "listener_left",
		"user_id":    username,
		"concurrent": conc,
	})
	return nil
}

// End closes a live session: marks ENDED, stops the egress recording and
// enqueues the worker job that converts it into a draft episode, persists
// aggregates, clears the creator's live flag, and disconnects the room.
func (s *Service) End(ctx context.Context, sessionID, userID string) (*Session, error) {
	session, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.CreatorID != s.mustCreatorID(ctx, userID) {
		return nil, ErrNotOwner
	}
	if session.Status != StatusLive {
		return nil, ErrInvalidStatus
	}

	conc, uniq, peak, err := s.presence.Stats(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	if err := s.store.MarkEnded(ctx, sessionID, peak, uniq); err != nil {
		return nil, err
	}
	_ = s.store.SetCreatorLive(ctx, session.CreatorID, false)

	s.finalizeRecording(ctx, sessionID)

	// NOTE: the room is NOT deleted here. Room-composite egress joins the room
	// as a participant and needs it alive to finalize/upload the recording;
	// deleting it now aborts the egress ("Start signal not received"). The
	// finalize task deletes the room once the file has landed. Listeners
	// disconnect on their own when the creator's track closes.
	s.broadcast(ctx, sessionID, map[string]any{
		"type":             "session_ended",
		"peak":             peak,
		"total":            uniq,
		"final_concurrent": conc,
	})
	s.store.AppendEvent(ctx, sessionID, "session.ended",
		[]byte(fmt.Sprintf(`{"peak":%d,"total":%d}`, peak, uniq)))

	fresh, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return fresh, nil
}

// StartRecordingNow begins the session recording via audio-only
// room-composite egress. Called by the creator's client shortly after the
// room connection is live. The egress service renders the room itself, so it
// does not depend on a specific participant or track (and survives the
// creator's client disconnecting). Recording continues until End.
// Idempotent: if a recording is already active for the session, it returns it.
func (s *Service) StartRecordingNow(ctx context.Context, sessionID, userID string) (*Recording, error) {
	session, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.CreatorID != s.mustCreatorID(ctx, userID) {
		return nil, ErrNotOwner
	}
	if session.Status != StatusLive {
		return nil, ErrInvalidStatus
	}
	if s.recorder == nil {
		return nil, ErrRecordingUnavailable
	}

	// Idempotency: reuse an active recording instead of stacking egress jobs.
	if rec, err := s.recordings.LatestRecording(ctx, sessionID); err == nil &&
		(rec.Status == RecordingStatusRecording || rec.Status == RecordingStatusEnding) {
		return rec, nil
	}

	roomName := "live-" + sessionID
	egressID, err := s.recorder.StartRecording(ctx, roomName)
	if err != nil {
		s.log.Warn("room recording failed to start",
			slog.String("session", sessionID), slog.Any("error", err))
		return nil, err
	}

	rec, err := s.recordings.CreateRecording(ctx, sessionID, egressID, roomName)
	if err != nil {
		s.log.Error("recording row insert failed",
			slog.String("session", sessionID), slog.Any("error", err))
		return nil, err
	}
	s.store.AppendEvent(ctx, sessionID, "recording.started",
		[]byte(fmt.Sprintf(`{"egress_id":%q,"mode":"room_composite"}`, egressID)))
	s.log.Info("recording started",
		slog.String("session", sessionID), slog.String("egress", egressID))
	return rec, nil
}

// Recording returns the latest recording for a session (studio status view).
func (s *Service) Recording(ctx context.Context, sessionID, userID string) (*Recording, error) {
	session, err := s.store.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if session.CreatorID != s.mustCreatorID(ctx, userID) {
		return nil, ErrNotOwner
	}
	return s.recordings.LatestRecording(ctx, sessionID)
}

// finalizeRecording stops the egress, marks the recording ENDING, and hands
// off to the worker. Best-effort: never blocks ending the stream.
func (s *Service) finalizeRecording(ctx context.Context, sessionID string) {
	rec, err := s.recordings.LatestRecording(ctx, sessionID)
	if err != nil || rec.Status != RecordingStatusRecording {
		return // no recording this session (or already finalized)
	}

	if s.recorder != nil {
		if err := s.recorder.StopRecording(ctx, rec.EgressID); err != nil {
			s.log.Warn("egress stop failed; worker will poll status",
				slog.String("session", sessionID), slog.String("egress", rec.EgressID), slog.Any("error", err))
		}
	} else {
		return // nothing can stop or convert the recording
	}
	if err := s.recordings.MarkRecordingEnded(ctx, rec.EgressID); err != nil {
		s.log.Error("recording ENDING update failed",
			slog.String("session", sessionID), slog.Any("error", err))
		return
	}
	s.store.AppendEvent(ctx, sessionID, "recording.stopping",
		[]byte(fmt.Sprintf(`{"egress_id":%q}`, rec.EgressID)))

	if s.enqueuer != nil {
		if err := s.enqueuer.EnqueueRecordingFinalize(ctx, sessionID, rec.EgressID); err != nil {
			s.log.Error("finalize enqueue failed; recording may need manual retry",
				slog.String("session", sessionID), slog.String("egress", rec.EgressID), slog.Any("error", err))
		}
	}
}

// Get returns one session.
func (s *Service) Get(ctx context.Context, sessionID string) (*Session, error) {
	return s.store.Get(ctx, sessionID)
}

// ListLive returns public LIVE sessions.
func (s *Service) ListLive(ctx context.Context, limit int) ([]Session, error) {
	return s.store.ListLive(ctx, limit)
}

// ListByCreator lists sessions for the studio.
func (s *Service) ListByCreator(ctx context.Context, userID string) ([]Session, error) {
	creatorID, err := s.store.CreatorIDByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.store.ListByCreator(ctx, creatorID, 50)
}

// Stats exposes presence numbers for the room UI.
func (s *Service) Stats(ctx context.Context, sessionID string) (concurrent int, unique int64, peak int, err error) {
	return s.presence.Stats(ctx, sessionID)
}

// Publish fans out an arbitrary room event (chat, reactions, moderation).
func (s *Service) Publish(ctx context.Context, sessionID string, payload []byte) error {
	return s.presence.Publish(ctx, sessionID, payload)
}

// Subscribe exposes the room channel to the ws-gateway.
func (s *Service) Subscribe(ctx context.Context, sessionID string) *redis.PubSub {
	return s.presence.Subscribe(ctx, sessionID)
}

func (s *Service) mustCreatorID(ctx context.Context, userID string) string {
	id, err := s.store.CreatorIDByUserID(ctx, userID)
	if err != nil {
		return ""
	}
	return id
}

func (s *Service) broadcast(ctx context.Context, sessionID string, payload map[string]any) {
	if err := s.presence.Publish(ctx, sessionID, toJSON(payload)); err != nil {
		s.log.Warn("room broadcast failed",
			slog.String("session", sessionID), slog.Any("error", err))
	}
}

func toJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}
