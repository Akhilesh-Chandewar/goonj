package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/live"
	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/notifications"
	searchmod "github.com/Akhilesh-Chandewar/goonj/apps/api/internal/modules/search"
)

// startDiscoveryLoops runs two periodic jobs until ctx is cancelled:
//   - trending recompute (default every 15 min, TRENDING_INTERVAL to override)
//   - live presence sampling (every 60s while sessions are LIVE; the rollup
//     happens when a session ends)
func startDiscoveryLoops(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) {
	interval := 15 * time.Minute
	if v := os.Getenv("TRENDING_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= time.Minute {
			interval = d
		}
	}
	go func() {
		// Prime immediately so /trending is never empty after deploy.
		if err := searchmod.RecomputeTrending(ctx, pool, rdb, log); err != nil {
			log.Warn("trending recompute failed", slog.Any("error", err))
		}
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := searchmod.RecomputeTrending(ctx, pool, rdb, log); err != nil {
					log.Warn("trending recompute failed", slog.Any("error", err))
				}
			}
		}
	}()

	go samplePresenceLoop(ctx, pool, rdb, log)
	go reminderLoop(ctx, pool, log)
}

// reminderLoop sends "going live soon" notifications to followers of a
// scheduled session ~10 minutes before its planned start. Dedupe marker is a
// 'reminder.sent' live_event, so a reminder fires exactly once per session.
func reminderLoop(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	liveStore := live.NewStore(pool)
	notify := notifications.NewService(notifications.NewStore(pool), log)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sessions, err := liveStore.ReminderDueSessions(ctx, 10*time.Minute)
			if err != nil {
				log.Warn("reminder query failed", slog.Any("error", err))
				continue
			}
			for _, sess := range sessions {
				if sess.ScheduledAt == nil {
					continue
				}
				notify.NotifySessionReminder(ctx, sess.CreatorID, sess.CreatorName,
					sess.ID, sess.Title, *sess.ScheduledAt)
				if err := liveStore.MarkReminderSent(ctx, sess.ID); err != nil {
					log.Warn("reminder marker failed",
						slog.String("session", sess.ID), slog.Any("error", err))
				}
			}
		}
	}
}

// samplePresenceLoop snapshots Redis presence into live_presence_samples so
// avg_concurrent can be rolled up at stream end (PLAN §5: snapshots only,
// never per-event DB writes — one INSERT per minute per live session).
func samplePresenceLoop(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, log *slog.Logger) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			rows, err := pool.Query(ctx, `
				SELECT id::text FROM live_sessions WHERE status = 'LIVE'`)
			if err != nil {
				log.Warn("presence sample: list live failed", slog.Any("error", err))
				continue
			}
			var live []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err == nil {
					live = append(live, id)
				}
			}
			rows.Close()
			for _, sid := range live {
				// Presence counters live in Redis (live module convention).
				var conc int
				key := "goonj:live:" + sid + ":concurrent"
				if n, err := rdb.Get(ctx, key).Int(); err == nil {
					conc = n
				}
				if _, err := pool.Exec(ctx, `
					INSERT INTO live_presence_samples (session_id, concurrent)
					VALUES ($1, $2)`, sid, conc); err != nil {
					log.Warn("presence sample insert failed",
						slog.String("session", sid), slog.Any("error", err))
				}
			}
		}
	}
}

// RollupLiveAnalytics aggregates a finished session's samples + counts into
// live_analytics and notifies followers that the recording became a draft
// episode. Called by the finalize-recording task after MarkConverted.
func RollupLiveAnalytics(ctx context.Context, pool *pgxpool.Pool, rdb *redis.Client, log *slog.Logger, sessionID, audioID string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO live_analytics (session_id, peak_concurrent, total_unique, avg_concurrent, duration_ms, updated_at)
		SELECT s.session_id,
			COALESCE(s.peak, 0), COALESCE(s.total, 0),
			COALESCE((SELECT round(avg(concurrent)) FROM live_presence_samples p WHERE p.session_id = s.id), 0),
			COALESCE(EXTRACT(EPOCH FROM (COALESCE(s.ended_at, now()) - s.started_at)) * 1000, 0)::int,
			now()
		FROM live_sessions s
		LEFT JOIN live_streams st ON st.session_id = s.id
		WHERE s.id = $1
		ON CONFLICT (session_id) DO UPDATE SET
			peak_concurrent = EXCLUDED.peak_concurrent,
			total_unique = EXCLUDED.total_unique,
			avg_concurrent = EXCLUDED.avg_concurrent,
			duration_ms = EXCLUDED.duration_ms,
			updated_at = now()`, sessionID)
	if err != nil {
		return fmt.Errorf("rollup analytics: %w", err)
	}
	log.Info("live analytics rolled up",
		slog.String("session", sessionID), slog.String("audio", audioID))
	return nil
}


