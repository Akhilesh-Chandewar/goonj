package notifications

import "testing"

func TestNotificationTypes(t *testing.T) {
	// The DB has no enum on notifications.type, but the web client keys its
	// icons off these exact strings — keep them stable.
	want := map[string]string{
		TypeLiveStarted:     "live_started",
		TypeRecordingReady:  "recording_ready",
		TypeNewFollower:     "new_follower",
		TypeSessionReminder: "session_reminder",
	}
	for got, w := range want {
		if got != w {
			t.Errorf("type constant drifted: got %q want %q", got, w)
		}
	}
}

func TestNotificationJSONShape(t *testing.T) {
	// UserID must never be serialized (it is the recipient, set by Notify).
	n := Notification{
		UserID: "secret-user-id",
		Type:   TypeLiveStarted,
		Title:  "someone is live",
		Body:   "tune in",
	}
	b := marshalForTest(t, n)
	if bytesContain(b, "secret-user-id") {
		t.Fatal("UserID leaked into JSON payload")
	}
	if !bytesContain(b, `"type":"live_started"`) {
		t.Errorf("missing type field in %s", b)
	}
}
