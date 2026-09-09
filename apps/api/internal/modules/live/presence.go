package live

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis key layout (per session id):
//
//	goonj:live:{id}:listeners   SET   unique listener user ids (TTL refreshed)
//	goonj:live:{id}:concurrent  STRING concurrent listener count
//	goonj:live:{id}:hb:{uid}    STRING heartbeat TTL key per listener
//	goonj:live:{id}:peak        STRING peak concurrent this session
//	goonj:live:{id}:ch          PUBSUB channel for room events
const (
	listenersKey = "goonj:live:%s:listeners"
	concKey      = "goonj:live:%s:concurrent"
	hbKeyFmt     = "goonj:live:%s:hb:%s"
	peakKey      = "goonj:live:%s:peak"
	channelKey   = "goonj:live:%s:ch"

	hbTTL       = 45 * time.Second
	presenceTTL = 24 * time.Hour
)

// Presence manages real-time listener state in Redis.
type Presence struct {
	rdb *redis.Client
	log *slog.Logger
}

func NewPresence(rdb *redis.Client, log *slog.Logger) *Presence {
	return &Presence{rdb: rdb, log: log}
}

// Join registers a listener atomically: adds to the unique set, bumps
// concurrency, updates the peak, and starts the heartbeat. One Lua script so
// first-join (missing keys) and peak races are handled correctly.
func (p *Presence) Join(ctx context.Context, sessionID, userID string) (concurrent int, unique int64, err error) {
	res, err := p.rdb.Eval(ctx, joinScript, []string{
		fmt.Sprintf(listenersKey, sessionID),
		fmt.Sprintf(concKey, sessionID),
		fmt.Sprintf(peakKey, sessionID),
		fmt.Sprintf(hbKeyFmt, sessionID, userID),
	}, userID, int(presenceTTL.Seconds()), int(hbTTL.Seconds())).Result()
	if err != nil {
		return 0, 0, err
	}
	parts, ok := res.([]any)
	if !ok || len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected join script response: %v", res)
	}
	conc, _ := strconv.Atoi(fmt.Sprintf("%v", parts[0]))
	uniq, _ := strconv.ParseInt(fmt.Sprintf("%v", parts[1]), 10, 64)
	return conc, uniq, nil
}

const joinScript = `
redis.call('SADD', KEYS[1], ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
local cur = tonumber(redis.call('GET', KEYS[2]) or '0') + 1
redis.call('SET', KEYS[2], cur, 'EX', ARGV[2])
local peak = tonumber(redis.call('GET', KEYS[3]) or '0')
if cur > peak then
  redis.call('SET', KEYS[3], cur, 'EX', ARGV[2])
end
redis.call('SET', KEYS[4], '1', 'EX', ARGV[3])
return {tostring(cur), redis.call('SCARD', KEYS[1])}`

// Heartbeat extends a listener's liveness TTL.
func (p *Presence) Heartbeat(ctx context.Context, sessionID, userID string) error {
	return p.rdb.Set(ctx, fmt.Sprintf(hbKeyFmt, sessionID, userID), "1", hbTTL).Err()
}

// Leave removes a listener's liveness and decrements concurrency (floor 0).
// The unique-listener set is intentionally kept: SCard = all-time unique
// listeners for the session total, not current concurrency.
func (p *Presence) Leave(ctx context.Context, sessionID, userID string) (int, error) {
	pipe := p.rdb.TxPipeline()
	pipe.Del(ctx, fmt.Sprintf(hbKeyFmt, sessionID, userID))
	pipe.Eval(ctx, `
		local cur = tonumber(redis.call('GET', KEYS[1]) or '0')
		if cur > 0 then redis.call('DECR', KEYS[1]) end
		return redis.call('GET', KEYS[1])`,
		[]string{fmt.Sprintf(concKey, sessionID)})
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return p.Concurrent(ctx, sessionID)
}

// Concurrent returns the current concurrent listener count.
func (p *Presence) Concurrent(ctx context.Context, sessionID string) (int, error) {
	n, err := p.rdb.Get(ctx, fmt.Sprintf(concKey, sessionID)).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return n, err
}

// Stats returns a point-in-time snapshot for the session.
func (p *Presence) Stats(ctx context.Context, sessionID string) (concurrent int, unique int64, peak int, err error) {
	pipe := p.rdb.Pipeline()
	concCmd := pipe.Get(ctx, fmt.Sprintf(concKey, sessionID))
	uniqCmd := pipe.SCard(ctx, fmt.Sprintf(listenersKey, sessionID))
	peakCmd := pipe.Get(ctx, fmt.Sprintf(peakKey, sessionID))
	if _, err = pipe.Exec(ctx); err != nil && err != redis.Nil {
		return 0, 0, 0, err
	}
	concurrent, _ = concCmd.Int()
	unique, _ = uniqCmd.Result()
	peak, _ = peakCmd.Int()
	return concurrent, unique, peak, nil
}

// Publish fans out a room event on the session's pub/sub channel.
func (p *Presence) Publish(ctx context.Context, sessionID string, payload []byte) error {
	return p.rdb.Publish(ctx, fmt.Sprintf(channelKey, sessionID), payload).Err()
}

// Subscribe listens to a session's room events channel.
func (p *Presence) Subscribe(ctx context.Context, sessionID string) *redis.PubSub {
	return p.rdb.Subscribe(ctx, fmt.Sprintf(channelKey, sessionID))
}
