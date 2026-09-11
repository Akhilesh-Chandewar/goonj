package playlists

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Playlist is a user-owned ordered collection of audio.
type Playlist struct {
	ID          string     `json:"id"`
	UserID      string     `json:"user_id"`
	OwnerName   string     `json:"owner_name,omitempty"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Visibility  string     `json:"visibility"`
	ItemCount   int        `json:"item_count"`
	Items       []Item     `json:"items,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `json:"-"`
}

// Item is one audio entry in a playlist.
type Item struct {
	AudioID     string     `json:"audio_id"`
	Position    int        `json:"position"`
	AddedAt     time.Time  `json:"added_at"`
	Title       string     `json:"title,omitempty"`
	CreatorName string     `json:"creator_name,omitempty"`
	Handle      string     `json:"handle,omitempty"`
	DurationMs  int        `json:"duration_ms"`
	Category    string     `json:"category,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// ErrNotFound when the playlist does not exist or is hidden from the caller.
var ErrNotFound = errors.New("playlist not found")

// Store is the Postgres repository for playlists.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Create inserts a playlist owned by userID.
func (st *Store) Create(ctx context.Context, userID, title, description, visibility string) (*Playlist, error) {
	row := st.pool.QueryRow(ctx, `
		INSERT INTO playlists (user_id, title, description, visibility)
		VALUES ($1, $2, $3, $4)
		RETURNING id, user_id, title, description, visibility,
			0, created_at, updated_at, deleted_at, ''`,
		userID, title, description, visibility)
	return scanPlaylist(row)
}

// Get fetches one playlist. Public playlists are readable by everyone;
// private ones only by their owner (ownerID may be empty for anonymous).
func (st *Store) Get(ctx context.Context, id, ownerID string) (*Playlist, error) {
	row := st.pool.QueryRow(ctx, `
		SELECT p.id, p.user_id, p.title, p.description, p.visibility,
			(SELECT count(*) FROM playlist_items pi WHERE pi.playlist_id = p.id),
			p.created_at, p.updated_at, p.deleted_at,
			COALESCE(pr.username, '')
		FROM playlists p
		LEFT JOIN profiles pr ON pr.user_id = p.user_id
		WHERE p.id = $1 AND p.deleted_at IS NULL`, id)
	pl, err := scanPlaylist(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if pl.Visibility != "public" && pl.UserID != ownerID {
		return nil, ErrNotFound // do not leak private playlists
	}
	return pl, nil
}

// ListByOwner returns the owner's playlists, most recently touched first.
func (st *Store) ListByOwner(ctx context.Context, userID string) ([]Playlist, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT p.id, p.user_id, p.title, p.description, p.visibility,
			(SELECT count(*) FROM playlist_items pi WHERE pi.playlist_id = p.id),
			p.created_at, p.updated_at, p.deleted_at,
			COALESCE(pr.username, '')
		FROM playlists p
		LEFT JOIN profiles pr ON pr.user_id = p.user_id
		WHERE p.user_id = $1 AND p.deleted_at IS NULL
		ORDER BY p.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Playlist
	for rows.Next() {
		pl, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *pl)
	}
	return out, rows.Err()
}

// UpdateMeta rewrites title/description/visibility for the owner.
func (st *Store) UpdateMeta(ctx context.Context, id, userID, title, description, visibility string) error {
	tag, err := st.pool.Exec(ctx, `
		UPDATE playlists SET title = $3, description = $4, visibility = $5, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		id, userID, title, description, visibility)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete soft-deletes an owned playlist.
func (st *Store) Delete(ctx context.Context, id, userID string) error {
	tag, err := st.pool.Exec(ctx,
		`UPDATE playlists SET deleted_at = now() WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Items returns a playlist's items in order, joined with playable metadata.
func (st *Store) Items(ctx context.Context, playlistID string) ([]Item, error) {
	rows, err := st.pool.Query(ctx, `
		SELECT pi.audio_id, pi.position, pi.added_at,
			COALESCE(a.title, ''), COALESCE(c.channel_name, ''), COALESCE(p.username, ''),
			COALESCE(m.duration_ms, 0), a.category, a.published_at
		FROM playlist_items pi
		JOIN audio a ON a.id = pi.audio_id
		LEFT JOIN audio_metadata m ON m.audio_id = a.id
		LEFT JOIN creators c ON c.id = a.creator_id
		LEFT JOIN profiles p ON p.user_id = c.user_id
		WHERE pi.playlist_id = $1
		ORDER BY pi.position, pi.added_at`, playlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.AudioID, &it.Position, &it.AddedAt, &it.Title,
			&it.CreatorName, &it.Handle, &it.DurationMs, &it.Category, &it.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AddItem appends audio to the end of a playlist (idempotent per audio).
func (st *Store) AddItem(ctx context.Context, playlistID, audioID string) (bool, error) {
	tag, err := st.pool.Exec(ctx, `
		INSERT INTO playlist_items (playlist_id, audio_id, position)
		SELECT $1, $2, COALESCE(MAX(position) + 1, 0) FROM playlist_items WHERE playlist_id = $1
		ON CONFLICT (playlist_id, audio_id) DO NOTHING`, playlistID, audioID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		_, err = st.pool.Exec(ctx,
			`UPDATE playlists SET updated_at = now() WHERE id = $1`, playlistID)
		return true, err
	}
	return false, nil
}

// RemoveItem deletes one item and renumbers the remaining positions in a
// single transaction so the ordering stays dense.
func (st *Store) RemoveItem(ctx context.Context, playlistID, audioID string) (bool, error) {
	tx, err := st.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`DELETE FROM playlist_items WHERE playlist_id = $1 AND audio_id = $2`,
		playlistID, audioID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		WITH ordered AS (
			SELECT id, row_number() OVER (ORDER BY position, added_at) - 1 AS pos
			FROM playlist_items WHERE playlist_id = $1
		)
		UPDATE playlist_items pi SET position = o.pos
		FROM ordered o WHERE pi.id = o.id`, playlistID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE playlists SET updated_at = now() WHERE id = $1`, playlistID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func scanPlaylist(row pgx.Row) (*Playlist, error) {
	var p Playlist
	err := row.Scan(&p.ID, &p.UserID, &p.Title, &p.Description, &p.Visibility,
		&p.ItemCount, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.OwnerName)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
