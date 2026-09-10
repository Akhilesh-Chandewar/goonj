// Command ws-gateway runs the Goonj WebSocket gateway for live sessions:
// chat, presence counts, reactions and moderation events, fanned out via
// Redis pub/sub. Raw audio never flows through WebSockets — listeners get
// audio over WebRTC from LiveKit; this gateway carries control/chat traffic.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/redis/go-redis/v9"

	"github.com/Akhilesh-Chandewar/goonj/apps/api/internal/platform"
)

// Envelope is the wire format for all room messages.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ChatSend is the client→server chat payload.
type ChatSend struct {
	Body string `json:"body"`
}

// ReactionSend is the client→server reaction payload.
type ReactionSend struct {
	Reaction string `json:"reaction"`
}

// ChatMessage is the server→clients chat payload.
type ChatMessage struct {
	ID        string `json:"id,omitempty"`
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Body      string `json:"body"`
	Timestamp int64  `json:"ts"`
}

func main() {
	if err := run(); err != nil {
		slog.Error("ws-gateway exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	cfg := platform.Load("goonj-ws")
	logger := platform.NewLogger(cfg)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rdb, err := platform.NewRedis(ctx, cfg, logger)
	if err != nil {
		return err
	}

	hub := newHub(rdb, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"alive"}`))
	})
	mux.HandleFunc("/ws/live/", func(w http.ResponseWriter, r *http.Request) {
		serveRoom(hub, w, r, logger)
	})

	srv := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("ws-gateway listening", slog.String("addr", srv.Addr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// serveRoom upgrades and hands the connection to the hub. Route shape:
// /ws/live/{sessionID}. Auth: bearer token in the access_token query param
// (browsers cannot set headers on WebSocket).
func serveRoom(hub *hub, w http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	sessionID := r.PathValue("sessionID")
	if sessionID == "" {
		// chi-less mux: parse manually from path.
		const prefix = "/ws/live/"
		p := r.URL.Path
		if len(p) > len(prefix) {
			sessionID = p[len(prefix):]
		}
	}
	if sessionID == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	token := r.URL.Query().Get("access_token")
	userID, username, err := validateToken(token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		logger.Warn("websocket accept failed", slog.Any("error", err))
		return
	}
	hub.runSession(conn, sessionID, userID, username)
}

// validateToken parses a JWT locally. To avoid importing the auth module
// (and its pg/redis deps), the gateway shares the JWT secret from config and
// validates claims itself.
func validateToken(raw string) (userID, username string, err error) {
	claims, err := parseAccessToken(raw)
	if err != nil {
		return "", "", err
	}
	return claims.UserID, claims.Username, nil
}

// hub owns room subscriptions and connection registries per session.
type hub struct {
	rdb   *redis.Client
	log   *slog.Logger
	rooms map[string]*room
}

func newHub(rdb *redis.Client, log *slog.Logger) *hub {
	return &hub{rdb: rdb, log: log, rooms: map[string]*room{}}
}

// room aggregates all sockets for one live session.
type room struct {
	id     string
	conns  map[*wsConn]struct{}
	pubsub *redis.PubSub
	cancel context.CancelFunc
}

// wsConn is one user socket.
type wsConn struct {
	conn     *websocket.Conn
	userID   string
	username string
	send     chan []byte
	room     *room
}

func (h *hub) runSession(conn *websocket.Conn, sessionID, userID, username string) {
	c := &wsConn{conn: conn, userID: userID, username: username, send: make(chan []byte, 32)}
	h.joinRoom(c, sessionID)
	defer h.leaveRoom(c, sessionID)

	readCtx, readCancel := context.WithCancel(context.Background())
	defer readCancel()

	// Writer pump.
	go func() {
		for msg := range c.send {
			writeCtx, cancel := context.WithTimeout(readCtx, 5*time.Second)
			err := c.conn.Write(writeCtx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				_ = c.conn.Close(websocket.StatusInternalError, "write failed")
				readCancel()
				return
			}
		}
	}()

	// Reader loop: client→server messages.
	for {
		msgType, data, err := c.conn.Read(readCtx)
		if err != nil {
			return
		}
		if msgType != websocket.MessageText {
			continue
		}
		h.handleClientMessage(readCtx, sessionID, c, data)
	}
}

func (h *hub) joinRoom(c *wsConn, sessionID string) {
	r, ok := h.rooms[sessionID]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		ps := h.rdb.Subscribe(ctx, roomChannel(sessionID))
		r = &room{id: sessionID, conns: map[*wsConn]struct{}{}, pubsub: ps, cancel: cancel}
		h.rooms[sessionID] = r

		// Redis→sockets pump.
		go func() {
			defer func() {
				_ = ps.Close()
				cancel()
			}()
			ch := ps.Channel()
			for {
				select {
				case msg, open := <-ch:
					if !open {
						return
					}
					h.broadcastLocal(r, []byte(msg.Payload))
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	r.conns[c] = struct{}{}
	c.room = r
}

func (h *hub) leaveRoom(c *wsConn, sessionID string) {
	r, ok := h.rooms[sessionID]
	if !ok {
		return
	}
	delete(r.conns, c)
	_ = c.conn.CloseNow()
	if len(r.conns) == 0 {
		r.cancel()
		delete(h.rooms, sessionID)
	}
}

func (h *hub) broadcastLocal(r *room, msg []byte) {
	for c := range r.conns {
		select {
		case c.send <- msg:
		default: // drop on full buffer to protect the hub
		}
	}
}

// handleClientMessage processes chat and reactions from a socket.
// Chat is persisted asynchronously: broadcast first, store best-effort.
func (h *hub) handleClientMessage(ctx context.Context, sessionID string, c *wsConn, data []byte) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return
	}

	switch env.Type {
	case "chat":
		var body ChatSend
		if err := json.Unmarshal(env.Payload, &body); err != nil || body.Body == "" || len(body.Body) > 500 {
			return
		}
		msg := ChatMessage{UserID: c.userID, Username: c.username, Body: body.Body, Timestamp: time.Now().UnixMilli()}
		out, _ := json.Marshal(map[string]any{"type": "chat", "payload": msg})
		if r, ok := h.rooms[sessionID]; ok {
			h.broadcastLocal(r, out)
		}
		_ = h.rdb.Publish(ctx, roomChannel(sessionID), out).Err()
		// Async persistence (fire-and-forget; Phase 3 moves to Asynq task).
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = h.rdb.RPush(bgCtx, "goonj:chat:persist", map[string]any{
				"session_id": sessionID, "user_id": c.userID, "body": body.Body,
			}).Result()
		}()

	case "reaction":
		var re ReactionSend
		if err := json.Unmarshal(env.Payload, &re); err != nil {
			return
		}
		if !validReaction(re.Reaction) {
			return
		}
		// Reactions are never persisted individually; just fan out.
		out, _ := json.Marshal(map[string]any{
			"type": "reaction", "payload": map[string]any{"reaction": re.Reaction, "by": c.username},
		})
		if r, ok := h.rooms[sessionID]; ok {
			h.broadcastLocal(r, out)
		}
		_ = h.rdb.Publish(ctx, roomChannel(sessionID), out).Err()
	}
}

func roomChannel(sessionID string) string { return "goonj:live:" + sessionID + ":ch" }

func validReaction(r string) bool {
	switch r {
	case "heart", "clap", "fire", "laugh", "party", "thumbsup":
		return true
	}
	return false
}
