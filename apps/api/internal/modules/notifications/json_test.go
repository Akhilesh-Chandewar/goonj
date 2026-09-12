package notifications

import (
	"encoding/json"
	"strings"
	"testing"
)

func marshalForTest(t *testing.T, n Notification) string {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func bytesContain(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
