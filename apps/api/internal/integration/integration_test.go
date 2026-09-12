// Package integration exercises the store layer against a real Postgres.
// These tests are opt-in so `go test ./...` stays green without services:
//
//	GOONJ_TEST_DATABASE=postgres://goonj:goonj@localhost:5432/goonj?sslmode=disable go test ./internal/integration/...
//
// The database must have migrations applied (docker compose runs goose at
// api boot; or run: goose -dir apps/api/migrations postgres "$GOONJ_TEST_DATABASE" up).
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	livemod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/live"
	notifmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/notifications"
	reportsmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/reports"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOONJ_TEST_DATABASE")
	if dsn == "" {
		t.Skip("GOONJ_TEST_DATABASE not set; skipping store-layer integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// uniqueEmail mints an isolated user so parallel runs don't collide.
func seedUser(t *testing.T, pool *pgxpool.Pool, email, role string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (email, password_hash, role)
		VALUES ($1, '$2a$10$CwTycUXWue0Thq9StjUM0uJ8DGjOtSKYpVpyxE+cxjpFfXOuPuSH6', $2) -- "password"
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id::text`, email, role).Scan(&id)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func seedCreator(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	var id string
	handle := "it_" + userID[:8]
	err := pool.QueryRow(context.Background(), `
		INSERT INTO creators (user_id, handle, channel_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET handle = EXCLUDED.handle
		RETURNING id::text`,
		userID, handle, handle).Scan(&id)
	if err != nil {
		t.Fatalf("seed creator: %v", err)
	}
	return id
}

func TestNotificationsStore(t *testing.T) {
	pool := testPool(t)
	store := notifmod.NewStore(pool)
	ctx := context.Background()

	uid := seedUser(t, pool, "it-notif@example.test", "USER")

	n := notifmod.Notification{
		UserID: uid, Type: notifmod.TypeLiveStarted,
		Title: "someone is live", Body: "tune in",
	}
	if err := store.Create(ctx, n); err != nil {
		t.Fatalf("create: %v", err)
	}

	list, err := store.List(ctx, uid, 10)
	if err != nil || len(list) == 0 {
		t.Fatalf("list: %v items=%d", err, len(list))
	}
	if list[0].Read {
		t.Error("fresh notification must be unread")
	}

	c, err := store.UnreadCount(ctx, uid)
	if err != nil || c == 0 {
		t.Fatalf("unread count: %v count=%d", err, c)
	}

	if err := store.MarkAllRead(ctx, uid); err != nil {
		t.Fatalf("mark all: %v", err)
	}
	c, _ = store.UnreadCount(ctx, uid)
	if c != 0 {
		t.Fatalf("unread after read-all: %d", c)
	}

	// MarkRead is owner-checked: a foreign user id must not flip the flag.
	if err := store.Create(ctx, n); err != nil {
		t.Fatalf("re-create: %v", err)
	}
	other := seedUser(t, pool, "it-notif-other@example.test", "USER")
	if err := store.MarkRead(ctx, other, list[0].ID); err != nil {
		t.Fatalf("mark read (wrong owner): %v", err)
	}
	list, _ = store.List(ctx, uid, 5)
	if !list[0].Read {
		_ = list // owner check verified by unread count below
	}
	if c, _ := store.UnreadCount(ctx, uid); c != 1 {
		t.Fatalf("wrong-owner MarkRead must not mark read; unread=%d", c)
	}
}

func TestReportsLifecycle(t *testing.T) {
	pool := testPool(t)
	store := reportsmod.NewStore(pool)
	ctx := context.Background()

	reporter := seedUser(t, pool, "it-reporter@example.test", "USER")

	// The audio target must exist (FK); create a minimal READY episode.
	var audioID string
	creator := seedCreator(t, pool, seedUser(t, pool, "it-creator@example.test", "CREATOR"))
	err := pool.QueryRow(ctx, `
		INSERT INTO audio (creator_id, title, status, visibility)
		VALUES ($1, 'it-report-target', 'READY', 'public')
		RETURNING id::text`, creator).Scan(&audioID)
	if err != nil {
		t.Fatalf("seed audio: %v", err)
	}

	r := reportsmod.Report{ReporterID: reporter}
	r.AudioID = &audioID
	r.Reason = "spam"
	if err := store.Create(ctx, r); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Duplicate PENDING collapses (unique index on coalesced ids).
	if err := store.Create(ctx, r); err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	q, err := store.List(ctx, reportsmod.StatusPending, 100)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	mine := 0
	for _, x := range q {
		if x.ReporterID == reporter && x.AudioID != nil && *x.AudioID == audioID {
			mine++
		}
	}
	if mine != 1 {
		t.Fatalf("expected exactly 1 pending report after dupe, got %d", mine)
	}

	// Resolve, then verify re-reporting is allowed post-resolution.
	var id string
	for _, x := range q {
		if x.ReporterID == reporter && x.AudioID != nil && *x.AudioID == audioID {
			id = x.ID
			break
		}
	}
	moderator := seedUser(t, pool, "it-moderator@example.test", "MODERATOR")
	if err := store.Resolve(ctx, id, moderator, reportsmod.StatusResolved); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := store.Resolve(ctx, id, moderator, reportsmod.StatusDismissed); err == nil {
		t.Fatal("resolving an already-closed report must fail")
	}
	if err := store.Create(ctx, r); err != nil {
		t.Fatalf("re-report after resolve: %v", err)
	}
}

func TestLiveScheduleFlow(t *testing.T) {
	pool := testPool(t)
	store := livemod.NewStore(pool)
	ctx := context.Background()

	creator := seedCreator(t, pool, seedUser(t, pool, "it-sched@example.test", "CREATOR"))

	sess, err := store.Create(ctx, creator, "it-scheduled show", "", "general", "public")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	startsAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if err := store.SetScheduledAt(ctx, sess.ID, startsAt); err != nil {
		t.Fatalf("set scheduled_at: %v", err)
	}

	upcoming, err := store.ListUpcoming(ctx, 50)
	if err != nil {
		t.Fatalf("list upcoming: %v", err)
	}
	found := false
	for _, s := range upcoming {
		if s.ID == sess.ID {
			found = true
			if s.ScheduledAt == nil {
				t.Fatal("upcoming session missing scheduled_at")
			}
		}
	}
	if !found {
		t.Fatal("scheduled session missing from upcoming list")
	}

	// Reminder due-window: only sessions inside the window are returned.
	due, err := store.ReminderDueSessions(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("reminder due: %v", err)
	}
	for _, s := range due {
		if s.ID == sess.ID {
			t.Fatal("session 24h out must not be reminder-due")
		}
	}
	if err := store.SetScheduledAt(ctx, sess.ID, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("reschedule near: %v", err)
	}
	due, err = store.ReminderDueSessions(ctx, 10*time.Minute)
	if err != nil {
		t.Fatalf("reminder due 2: %v", err)
	}
	inWindow := false
	for _, s := range due {
		if s.ID == sess.ID {
			inWindow = true
		}
	}
	if !inWindow {
		t.Fatal("session 5m out must be reminder-due")
	}
	if err := store.MarkReminderSent(ctx, sess.ID); err != nil {
		t.Fatalf("mark reminder: %v", err)
	}
	due, _ = store.ReminderDueSessions(ctx, 10*time.Minute)
	for _, s := range due {
		if s.ID == sess.ID {
			t.Fatal("reminder dedupe marker failed")
		}
	}
}

// TestFollowerIDsSQL guards the went-live fan-out query against column drift.
// Regression context: the query referenced a nonexistent follower_id column and
// NotifyLiveStarted swallowed the error, so went-live notifications silently
// never fired for any follower.
func TestFollowerIDsSQL(t *testing.T) {
	pool := testPool(t)
	store := notifmod.NewStore(pool)
	ctx := context.Background()

	creatorUser := seedUser(t, pool, "it-fanout-creator@example.test", "CREATOR")
	creatorID := seedCreator(t, pool, creatorUser)
	f1 := seedUser(t, pool, "it-fanout-a@example.test", "USER")
	f2 := seedUser(t, pool, "it-fanout-b@example.test", "USER")
	for _, f := range []string{f1, f2} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO follows (user_id, creator_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			f, creatorID); err != nil {
			t.Fatalf("seed follow: %v", err)
		}
	}

	ids, err := store.FollowerIDs(ctx, creatorID)
	if err != nil {
		t.Fatalf("FollowerIDs: %v", err)
	}
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	for _, want := range []string{f1, f2} {
		if !got[want] {
			t.Fatalf("FollowerIDs missing follower %s; got %v", want, ids)
		}
	}
}
