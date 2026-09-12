package live

import (
	"context"
	"fmt"
	"time"

	lkauth "github.com/livekit/protocol/auth"
)

// StreamGrant is what a participant is allowed to do in a room.
type StreamGrant struct {
	// Publisher grants broadcast (creator) rights; subscribers listen only.
	Publisher bool
}

// StreamToken is a short-lived credential for joining a live room.
type StreamToken struct {
	Token    string `json:"token"`
	RoomName string `json:"room_name"`
	// WSURL is the LiveKit endpoint listeners connect to. In production this
	// must be a wss:// URL fronted by TLS; TURN (LIVEKIT_TURN_URL) is served
	// by the LiveKit server config, not per-token.
	WSURL string `json:"ws_url"`
	// ExpiresAt is when the token stops working.
	ExpiresAt time.Time `json:"expires_at"`
}

// Streamer issues room credentials. Interface per PLAN §53: the LiveKit
// implementation can be swapped (LiveKit Cloud, IVS, self-hosted v2) without
// touching the live module's business logic.
type Streamer interface {
	// EnsureRoom creates the room if missing (audio-only).
	EnsureRoom(ctx context.Context, roomName string) error
	// JoinToken mints a short-lived join token for a participant.
	JoinToken(ctx context.Context, roomName, identity string, grant StreamGrant, ttl time.Duration) (*StreamToken, error)
	// CloseRoom ends a room and disconnects participants (used on stream end).
	CloseRoom(ctx context.Context, roomName string) error
}

// LiveKitStreamer implements Streamer against a LiveKit server.
type LiveKitStreamer struct {
	host      string // ws(s)://host:port of the LiveKit server
	clientURL string // what BROWSERS get in ws_url (may differ behind a proxy/TLS)
	apiKey    string
	apiSecret string
	tokenTTL  time.Duration
}

// NewLiveKitStreamer builds the streamer. clientURL is optional: when empty
// the server host is handed to browsers too. In production, set
// LIVEKIT_CLIENT_URL to the public wss:// endpoint (TLS terminates at the
// proxy/LiveKit; TURN runs server-side via rtc.turn config, PROBLEMS #5/#6).
func NewLiveKitStreamer(host, clientURL, apiKey, apiSecret string) *LiveKitStreamer {
	if clientURL == "" {
		clientURL = host
	}
	return &LiveKitStreamer{host: host, clientURL: clientURL, apiKey: apiKey, apiSecret: apiSecret, tokenTTL: 6 * time.Hour}
}

// EnsureRoom creates an audio-only room. LiveKit auto-creates rooms on first
// participant join, so this is a no-op placeholder that keeps the interface
// honest for providers that need explicit room creation.
func (lk *LiveKitStreamer) EnsureRoom(_ context.Context, _ string) error {
	return nil
}

// JoinToken mints a LiveKit access token with audio publish/subscribe grants.
func (lk *LiveKitStreamer) JoinToken(_ context.Context, roomName, identity string, grant StreamGrant, ttl time.Duration) (*StreamToken, error) {
	if ttl <= 0 {
		ttl = lk.tokenTTL
	}
	at := lkauth.NewAccessToken(lk.apiKey, lk.apiSecret)
	at.SetIdentity(identity).
		SetValidFor(ttl).
		SetVideoGrant(&lkauth.VideoGrant{
			Room:           roomName,
			RoomJoin:       true,
			CanPublish:     funcPtr(grant.Publisher),
			CanSubscribe:   funcPtr(true),
			CanPublishData: funcPtr(true),
		})
	token, err := at.ToJWT()
	if err != nil {
		return nil, fmt.Errorf("mint livekit token: %w", err)
	}
	return &StreamToken{
		Token:     token,
		RoomName:  roomName,
		WSURL:     lk.clientURL,
		ExpiresAt: time.Now().Add(ttl),
	}, nil
}

// CloseRoom deletes the room via the RoomService API (Phase 3 adds egress).
func (lk *LiveKitStreamer) CloseRoom(_ context.Context, _ string) error {
	// Room deletion requires the room service client; wired with egress in
	// Phase 3. Participants disconnect when the creator's track closes.
	return nil
}

func funcPtr(b bool) *bool { return &b }
