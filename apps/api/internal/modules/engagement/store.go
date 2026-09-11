package engagement

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Comment shape returned to clients. Counted rows (likes) are derived at read
// time so they can never drift from the truth.
type Comment struct {
	ID        string     `json:"id"`
	AudioID   string     `json:"audio_id"`
	UserID    string     `json:"user_id"`
	Username  string     `json:"username"`
	ParentID  *string    `json:"parent_id,omitempty"`
	Body      string     `json:"body"`
	IsPinned  bool       `json:"is_pinned"`
	LikeCount int64      `json:"like_count"`
	Mine      bool       `json:"mine"`
	CreatedAt time.Time  `json:"created_at"`
	DeletedAt *time.Time `json:"-"`
}

// ErrNotFound when the target does not exist or is hidden from the caller.
var ErrNotFound = errors.New("not found")

// Store is the Postgres repository for likes and comments.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// LikeAudio records a like; returns true if the like was newly created.
func (st *Store) LikeAudio(ctx context.Context, userID, audioID string) (bool, error) {
	tag, err := st.pool.Exec(ctx,
		`INSERT INTO audio_likes (user_id, audio_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, audio_id) DO NOTHING`, userID, audioID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UnlikeAudio removes a like; returns true if a like was actually removed.
func (st *Store) UnlikeAudio(ctx context.Context, userID, audioID string) (bool, error) {
	tag, err := st.pool.Exec(ctx,
		`DELETE FROM audio_likes WHERE user_id = $1 AND audio_id = $2`, userID, audioID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// AudioLikeCount counts likes on one audio row.
func (st *Store) AudioLikeCount(ctx context.Context, audioID string) (int64, error) {
	var n int64
	err := st.pool.QueryRow(ctx,
		`SELECT count(*) FROM audio_likes WHERE audio_id = $1`, audioID).Scan(&n)
	return n, err
}

// AudioLikedByUser reports the caller's like state.
func (st *Store) AudioLikedByUser(ctx context.Context, userID, audioID string) (bool, error) {
	var exists bool
	err := st.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM audio_likes WHERE user_id = $1 AND audio_id = $2)`,
		userID, audioID).Scan(&exists)
	return exists, err
}

// LikedAudio lists audio the user liked, newest like first (library view).
// Only public READY audio is returned — likes on drafts stay invisible.
func (st *Store) LikedAudio(ctx context.Context, userID string, limit, offset int) ([]map[string]any, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.category, a.published_at, a.created_at,
			COALESCE(m.duration_ms, 0),
			COALESCE(c.channel_name, ''), COALESCE(p.username, '')
		FROM audio_likes al
		JOIN audio a ON a.id = al.audio_id
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE al.user_id = $1
			AND a.status = 'READY' AND a.visibility = 'public' AND a.deleted_at IS NULL
		ORDER BY al.created_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var (
			id, title, category, creatorName, handle string
			duration                                 int
			publishedAt                              *time.Time
			createdAt                                time.Time
		)
		if err := rows.Scan(&id, &title, &category, &publishedAt, &createdAt,
			&duration, &creatorName, &handle); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "title": title, "category": category,
			"published_at": publishedAt, "created_at": createdAt,
			"duration_ms": duration,
			"creator_name": creatorName, "handle": handle,
		})
	}
	return out, rows.Err()
}

// InsertComment creates a comment or a reply (parent must belong to the same
// audio and be top-level — replies to replies flatten one level).
func (st *Store) InsertComment(ctx context.Context, audioID, userID string, parentID *string, body string) (*Comment, error) {
	if parentID != nil && *parentID != "" {
		var parentAudioID string
		var parentParent *string
		err := st.pool.QueryRow(ctx,
			`SELECT audio_id, parent_id FROM comments WHERE id = $1 AND deleted_at IS NULL`,
			*parentID).Scan(&parentAudioID, &parentParent)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if parentAudioID != audioID || parentParent != nil {
			return nil, ErrNotFound // reply target must be a top-level sibling comment
		}
	} else {
		parentID = nil
	}

	row := st.pool.QueryRow(ctx, `
		INSERT INTO comments (audio_id, user_id, parent_id, body)
		VALUES ($1, $2, $3, $4)
		RETURNING id, audio_id, user_id, '', parent_id, body, is_pinned,
			0, true, created_at, NULL::timestamptz`,
		audioID, userID, parentID, body)
	return scanComment(row)
}

// ListComments returns a comment thread for one audio: top-level comments
// newest-first with their replies nested oldest-first. viewerID may be empty
// (anonymous) — it is passed as nil so Postgres never sees uuid = text.
func (st *Store) ListComments(ctx context.Context, audioID, viewerID string, limit int) ([]*Comment, []*Comment, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var viewer *string
	if viewerID != "" {
		viewer = &viewerID
	}
	rows, err := st.pool.Query(ctx, `
		SELECT c.id, c.audio_id, c.user_id, COALESCE(pr.username, ''),
			c.parent_id, c.body, c.is_pinned,
			(SELECT count(*) FROM comment_likes cl WHERE cl.comment_id = c.id),
			($2::uuid IS NOT NULL AND c.user_id = $2::uuid),
			c.created_at, c.deleted_at
		FROM comments c
		LEFT JOIN profiles pr ON pr.user_id = c.user_id
		WHERE c.audio_id = $1 AND c.deleted_at IS NULL
		ORDER BY c.created_at DESC
		LIMIT $3`, audioID, viewer, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var all []*Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, nil, err
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	tops := make([]*Comment, 0, len(all))
	byParent := map[string][]*Comment{}
	for _, c := range all {
		if c.ParentID == nil {
			tops = append(tops, c)
		} else {
			byParent[*c.ParentID] = append(byParent[*c.ParentID], c)
		}
	}
	// Keep only replies whose top-level parent made the page cut.
	replies := make([]*Comment, 0, len(all)-len(tops))
	for _, t := range tops {
		for _, r := range byParent[t.ID] {
			replies = append(replies, r)
		}
	}
	return tops, replies, nil
}

// DeleteComment soft-deletes; callers enforce ownership.
func (st *Store) DeleteComment(ctx context.Context, id, userID string) (bool, error) {
	tag, err := st.pool.Exec(ctx,
		`UPDATE comments SET deleted_at = now(), updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`, id, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// LikeComment records a comment like; returns true when newly created.
func (st *Store) LikeComment(ctx context.Context, userID, commentID string) (bool, error) {
	tag, err := st.pool.Exec(ctx,
		`INSERT INTO comment_likes (user_id, comment_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, comment_id) DO NOTHING`, userID, commentID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// UnlikeComment removes a comment like.
func (st *Store) UnlikeComment(ctx context.Context, userID, commentID string) (bool, error) {
	tag, err := st.pool.Exec(ctx,
		`DELETE FROM comment_likes WHERE user_id = $1 AND comment_id = $2`, userID, commentID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func scanComment(row pgx.Row) (*Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.AudioID, &c.UserID, &c.Username, &c.ParentID, &c.Body,
		&c.IsPinned, &c.LikeCount, &c.Mine, &c.CreatedAt, &c.DeletedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
