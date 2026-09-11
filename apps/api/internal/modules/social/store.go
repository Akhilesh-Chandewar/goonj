package social

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Creator is a channel as shown on creator pages and follow lists.
type Creator struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Handle          string     `json:"handle"`
	ChannelName     string     `json:"channel_name"`
	Tagline         string     `json:"tagline"`
	IsLive          bool       `json:"is_live"`
	SubscriberCount int64      `json:"subscriber_count"`
	CreatedAt       time.Time  `json:"created_at"`
	DeletedAt       *time.Time `json:"-"`
}

// FeedItem is one episode in the subscriptions feed.
type FeedItem struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Category    string     `json:"category"`
	CreatorID   string     `json:"creator_id"`
	CreatorName string     `json:"creator_name"`
	Handle      string     `json:"handle"`
	DurationMs  int        `json:"duration_ms"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ErrNotFound when the creator or follow edge does not exist.
var ErrNotFound = errors.New("not found")

// Store is the Postgres repository for the follow graph.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Follow records a follow edge (idempotent) and increments the cached
// subscriber counter on creators.
func (st *Store) Follow(ctx context.Context, userID, creatorID string) error {
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`INSERT INTO follows (user_id, creator_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, creator_id) DO NOTHING`, userID, creatorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE creators SET subscriber_count = subscriber_count + 1
			 WHERE id = $1 AND deleted_at IS NULL`, creatorID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Unfollow removes the edge and decrements the counter (never below zero).
func (st *Store) Unfollow(ctx context.Context, userID, creatorID string) error {
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`DELETE FROM follows WHERE user_id = $1 AND creator_id = $2`, userID, creatorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE creators SET subscriber_count = GREATEST(subscriber_count - 1, 0)
			 WHERE id = $1`, creatorID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// IsFollowing reports the caller's follow state for a creator.
func (st *Store) IsFollowing(ctx context.Context, userID, creatorID string) (bool, error) {
	var exists bool
	err := st.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM follows WHERE user_id = $1 AND creator_id = $2)`,
		userID, creatorID).Scan(&exists)
	return exists, err
}

// GetCreator fetches one channel by id.
func (st *Store) GetCreator(ctx context.Context, id string) (*Creator, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT id, user_id, handle, channel_name, tagline, is_live,
			subscriber_count, created_at, deleted_at
		FROM creators WHERE id = $1 AND deleted_at IS NULL`, id)
	c, err := scanCreator(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ListFollowed returns creators the user follows (library view).
func (st *Store) ListFollowed(ctx context.Context, userID string) ([]Creator, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT c.id, c.user_id, c.handle, c.channel_name, c.tagline, c.is_live,
			c.subscriber_count, c.created_at, c.deleted_at
		FROM follows f
		JOIN creators c ON c.id = f.creator_id
		WHERE f.user_id = $1 AND c.deleted_at IS NULL
		ORDER BY f.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Creator
	for rows.Next() {
		c, err := scanCreator(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// SubscriptionsFeed lists newest public READY episodes from creators the user
// follows. Deduped per episode; published_at DESC is the radio ordering.
func (st *Store) SubscriptionsFeed(ctx context.Context, userID string, limit, offset int) ([]FeedItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.category, a.creator_id,
			COALESCE(c.channel_name, ''), COALESCE(p.username, ''),
			COALESCE(m.duration_ms, 0), a.published_at, a.created_at
		FROM follows f
		JOIN audio a ON a.creator_id = f.creator_id
			AND a.status = 'READY' AND a.visibility = 'public' AND a.deleted_at IS NULL
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE f.user_id = $1
		ORDER BY a.published_at DESC
		LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FeedItem
	for rows.Next() {
		var f FeedItem
		if err := rows.Scan(&f.ID, &f.Title, &f.Category, &f.CreatorID,
			&f.CreatorName, &f.Handle, &f.DurationMs, &f.PublishedAt, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreatorAudio lists a channel's public READY episodes (creator page).
func (st *Store) CreatorAudio(ctx context.Context, creatorID string, limit, offset int) ([]FeedItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 24
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := st.pool.Query(ctx, `
		SELECT a.id, a.title, a.category, a.creator_id,
			COALESCE(c.channel_name, ''), COALESCE(p.username, ''),
			COALESCE(m.duration_ms, 0), a.published_at, a.created_at
		FROM audio a
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE a.creator_id = $1 AND a.status = 'READY' AND a.visibility = 'public' AND a.deleted_at IS NULL
		ORDER BY a.published_at DESC
		LIMIT $2 OFFSET $3`, creatorID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FeedItem
	for rows.Next() {
		var f FeedItem
		if err := rows.Scan(&f.ID, &f.Title, &f.Category, &f.CreatorID,
			&f.CreatorName, &f.Handle, &f.DurationMs, &f.PublishedAt, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanCreator(row pgx.Row) (*Creator, error) {
	var c Creator
	err := row.Scan(&c.ID, &c.UserID, &c.Handle, &c.ChannelName, &c.Tagline, &c.IsLive,
		&c.SubscriberCount, &c.CreatedAt, &c.DeletedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
