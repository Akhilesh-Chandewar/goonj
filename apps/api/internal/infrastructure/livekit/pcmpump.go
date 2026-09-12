// Package livekit provides the server-side LiveKit client: room-composite
// egress recording of live audio rooms, plus the track-egress PCM pump used
// by the realtime translator.
package livekit

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// PCMPump is a tiny WebSocket server that LiveKit's TrackEgress streams raw
// audio into. Per LiveKit's track-egress docs, when a TrackEgress is started
// with a websocket_url output the egress service dials our URL and pushes:
//   - binary frames: raw PCM (pcm_s16le, mono, sample rate = the track's —
//     typically 48kHz);
//   - text frames: JSON events such as {"muted":true}/{"muted":false}.
//
// The connection closes when the track is unpublished or the speaker leaves.
// One pump serves ONE egress stream (one source track); the translator wraps
// it with per-language translation sessions. Mixing multiple speakers into
// one translated stream is out of scope by design.
type PCMPump struct {
	log *slog.Logger

	mu     sync.Mutex
	closed bool

	// frames carries PCM chunks in arrival order. Bounded: if the consumer
	// falls behind, oldest frames drop (translation stays realtime; quality
	// degrades instead of latency growing without bound).
	frames chan []byte
	// muted events (cap so a mute storm cannot backpressure the reader).
	muted chan bool

	lastData time.Time
	srv      *http.Server
	ready    chan struct{}
	conn     *websocket.Conn
	connMu   sync.Mutex
}

// pumpBuf bounds the in-flight PCM window: ~2s of 48kHz 16-bit mono (~192KB).
const pumpBuf = 400

// NewPCMPump starts listening on addr (e.g. ":9600") and returns immediately;
// the first egress connection marks it ready. The pump's URL must be
// reachable from the livekit-egress container — in compose that is this
// service's DNS name on the goonj_default network, never localhost.
func NewPCMPump(addr string, log *slog.Logger) *PCMPump {
	p := &PCMPump{
		log:    log,
		frames: make(chan []byte, pumpBuf),
		muted:  make(chan bool, 8),
		ready:  make(chan struct{}, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/egress", p.handleEgress)
	p.srv = &http.Server{Addr: addr, Handler: mux}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("pcm pump listen failed", slog.String("addr", addr), slog.Any("error", err))
		close(p.ready)
		return p
	}
	go func() {
		if err := p.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error("pcm pump serve failed", slog.Any("error", err))
		}
	}()
	return p
}

// URL returns the ws:// URL (host:port form passed in) with the /egress path
// the pump serves. Query params (track id, session) can be appended freely —
// LiveKit recommends passing identifiers through them.
func URL(hostPort string) string { return "ws://" + hostPort + "/egress" }

// Ready fires once the egress service has connected (or immediately if the
// listener failed to bind — the translator will log and exit its loop).
func (p *PCMPump) Ready() <-chan struct{} { return p.ready }

// Frames streams PCM chunks. The channel closes when the pump stops.
func (p *PCMPump) Frames() <-chan []byte { return p.frames }

// Muted streams mute/unmute events from the egress text frames.
func (p *PCMPump) Muted() <-chan bool { return p.muted }

// Stop shuts the HTTP server down and closes the egress socket.
func (p *PCMPump) Stop(ctx context.Context) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	_ = p.srv.Shutdown(ctx)
	p.connMu.Lock()
	if p.conn != nil {
		_ = p.conn.Close(websocket.StatusNormalClosure, "pump stopped")
	}
	p.connMu.Unlock()
}

func (p *PCMPump) handleEgress(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	p.connMu.Lock()
	p.conn = conn
	p.connMu.Unlock()

	p.log.Info("track egress connected",
		slog.String("remote", r.RemoteAddr), slog.String("query", r.URL.RawQuery))
	select {
	case p.ready <- struct{}{}:
	default:
	}

	defer func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "egress ended")
	}()

	for {
		// Long-lived stream; per-read timeout keeps a wedged socket from
		// pinning the goroutine forever.
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		msgType, raw, err := conn.Read(ctx)
		cancel()
		if err != nil {
			return // egress closed (track unpublished / speaker left) or timeout
		}
		p.mu.Lock()
		p.lastData = time.Now()
		p.mu.Unlock()

		if msgType == websocket.MessageText {
			var ev struct {
				Muted bool `json:"muted"`
			}
			if json.Unmarshal(raw, &ev) == nil {
				select {
				case p.muted <- ev.Muted:
				default: // drop-oldest: translation stays realtime
					select {
					case <-p.muted:
					default:
					}
					select {
					case p.muted <- ev.Muted:
					default:
					}
				}
			}
			continue
		}

		// Binary frame = PCM chunk. Copy: the websocket buffer is reused.
		chunk := make([]byte, len(raw))
		copy(chunk, raw)
		select {
		case p.frames <- chunk:
		default:
			// Consumer behind: drop the OLDEST frame to keep latency flat,
			// then try once more to admit this one.
			select {
			case <-p.frames:
			default:
			}
			select {
			case p.frames <- chunk:
			default:
			}
		}
	}
}
