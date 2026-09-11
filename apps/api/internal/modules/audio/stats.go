package audio

import (
	"context"
	"time"
)

// StudioStats summarizes one creator's channel for the studio dashboard.
// All numbers come from counted queries on indexed columns — analytics
// rollup tables (worker-owned) arrive in Phase 5 and will replace the
// engagement sums here, not the API shape.
type StudioStats struct {
	Episodes     int64            `json:"episodes"`
	TotalLikes   int64            `json:"total_likes"`
	TotalSeconds int64            `json:"total_seconds"`
	Latest       *Audio           `json:"latest,omitempty"`
	TopAudio     []TopAudioRow    `json:"top_audio"`
	Recent       []PublishRow     `json:"recent_publishes"`
	LiveSessions LiveSessionStats `json:"live_sessions"`
}

// TopAudioRow is one episode with its like count.
type TopAudioRow struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Likes     int64      `json:"likes"`
	Published *time.Time `json:"published_at,omitempty"`
	Duration  int        `json:"duration_ms"`
}

// PublishRow is one recently published episode.
type PublishRow struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Published *time.Time `json:"published_at,omitempty"`
}

// LiveSessionStats are totals over the creator's ended live sessions.
type LiveSessionStats struct {
	Total   int   `json:"total"`
	Peak    int   `json:"peak_listeners"`
	Watched int64 `json:"total_listeners"`
}

// StudioStats assembles the dashboard payload for a creator.
func (st *Store) StudioStats(ctx context.Context, creatorID string) (*StudioStats, error) {
	out := &StudioStats{TopAudio: []TopAudioRow{}, Recent: []PublishRow{}}

	if err := st.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE a.status = 'READY' AND a.visibility = 'public'),
			COALESCE(sum(m.duration_ms), 0)
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		WHERE a.creator_id = $1 AND a.deleted_at IS NULL`,
		creatorID).Scan(&out.Episodes, &out.TotalSeconds); err != nil {
		return nil, err
	}

	if err := st.pool.QueryRow(ctx, `
		SELECT count(*) FROM audio_likes al
		JOIN audio a ON a.id = al.audio_id
		WHERE a.creator_id = $1`, creatorID).Scan(&out.TotalLikes); err != nil {
		return nil, err
	}

	rows, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.published_at, COALESCE(m.duration_ms, 0),
			(SELECT count(*) FROM audio_likes al WHERE al.audio_id = a.id)
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		WHERE a.creator_id = $1 AND a.status = 'READY' AND a.visibility = 'public'
			AND a.deleted_at IS NULL
		ORDER BY 5 DESC, a.published_at DESC
		LIMIT 5`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t TopAudioRow
		if err := rows.Scan(&t.ID, &t.Title, &t.Published, &t.Duration, &t.Likes); err != nil {
			return nil, err
		}
		out.TopAudio = append(out.TopAudio, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.published_at
		FROM audio a
		WHERE a.creator_id = $1 AND a.status = 'READY' AND a.published_at IS NOT NULL
			AND a.deleted_at IS NULL
		ORDER BY a.published_at DESC
		LIMIT 5`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var p PublishRow
		if err := rows2.Scan(&p.ID, &p.Title, &p.Published); err != nil {
			return nil, err
		}
		out.Recent = append(out.Recent, p)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	live := st.pool.QueryRow(ctx, `
		SELECT count(*), COALESCE(max(peak_listeners), 0), COALESCE(sum(total_listeners), 0)
		FROM live_sessions WHERE creator_id = $1 AND status = 'ENDED'`, creatorID)
	if err := live.Scan(&out.LiveSessions.Total, &out.LiveSessions.Peak, &out.LiveSessions.Watched); err != nil {
		return nil, err
	}

	return out, nil
}

// LatestPublished returns the creator's newest public episode, if any.
func (st *Store) LatestPublished(ctx context.Context, creatorID string) (*Audio, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT `+audioColumns+`
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE a.creator_id = $1 AND a.status = 'READY' AND a.visibility = 'public'
			AND a.deleted_at IS NULL
		ORDER BY a.published_at DESC
		LIMIT 1`, creatorID)
	a, err := scanAudio(row)
	if err != nil {
		return nil, err // pgx.ErrNoRows surfaces as nil latest
	}
	return a, nil
}
