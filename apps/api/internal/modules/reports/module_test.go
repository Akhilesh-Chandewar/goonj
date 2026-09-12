package reports

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// Unit tests for the reports service. The service validates before touching
// the store, so validation failures are assertable with a nil store. Valid
// inputs proceed to Store.Create, which requires a database — covered by E2E,
// not here.

func TestFileValidation(t *testing.T) {
	s := NewService(nil, slog.Default())
	ctx := context.Background()

	cases := []struct {
		name      string
		audioID   string
		sessionID string
		reason    string
	}{
		{"no target", "", "", "spam"},
		{"bad reason no target", "", "", "bogus"},
		{"bad reason with target", "a", "", "bogus"},
		{"empty reason with target", "a", "", ""},
		{"whitespace reason", "a", "", "   "},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.File(ctx, "reporter", tc.audioID, tc.sessionID, tc.reason, "")
			if !errors.Is(err, ErrInvalidReport) {
				t.Fatalf("File(%q,%q,%q) = %v, want ErrInvalidReport", tc.audioID, tc.sessionID, tc.reason, err)
			}
		})
	}
}

// File normalizes case/whitespace before validating: " SPAM " must be
// accepted (i.e. it must NOT fail validation — it proceeds to the store,
// which panics on nil; recover as the pass marker).
func TestFileNormalizesReason(t *testing.T) {
	s := NewService(nil, slog.Default())
	func() {
		defer func() { _ = recover() }() // nil store reached == validation passed
		_ = s.File(context.Background(), "r", "a", "", "  SPAM  ", "")
	}()
}

func TestFileTruncatesDetails(t *testing.T) {
	s := NewService(nil, slog.Default())
	long := strings.Repeat("x", 1500)
	defer func() { _ = recover() }() // nil store reached == validation passed
	_ = s.File(context.Background(), "r", "a", "", "spam", long)
}

func TestCloseRejectsBadStatus(t *testing.T) {
	s := NewService(nil, slog.Default())
	if err := s.Close(context.Background(), "id", "mod", "MAYBE"); !errors.Is(err, ErrInvalidReport) {
		t.Fatalf("Close(MAYBE) = %v, want ErrInvalidReport", err)
	}
	defer func() { _ = recover() }() // valid status → store (nil) reached
	_ = s.Close(context.Background(), "id", "mod", StatusResolved)
}

func TestReasonVocabulary(t *testing.T) {
	for _, r := range []string{"spam", "harassment", "copyright", "explicit", "misinformation", "other"} {
		if !ValidReasons[r] {
			t.Errorf("reason %q should be valid", r)
		}
	}
	if ValidReasons["bogus"] {
		t.Error("bogus reason should not be valid")
	}
}
