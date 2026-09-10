package livekit

import (
	"fmt"
	"time"

	lkauth "github.com/livekit/protocol/auth"
)

// serverAccessToken mints the JWT LiveKit's server RPC APIs require. Unlike
// participant tokens (streamer.go), these carry no identity — only the admin
// grants needed for egress (roomRecord) and room management (roomAdmin).
func serverAccessToken(apiKey, apiSecret string) (string, error) {
	if apiKey == "" || apiSecret == "" {
		return "", fmt.Errorf("livekit: missing API key or secret")
	}
	at := lkauth.NewAccessToken(apiKey, apiSecret)
	at.SetValidFor(2 * time.Minute)
	at.SetVideoGrant(&lkauth.VideoGrant{
		// Server-side scope: unscoped admin across all rooms.
		RoomAdmin:  true,
		RoomRecord: true,
		RoomCreate: true,
		RoomList:   true,
	})
	return at.ToJWT()
}
