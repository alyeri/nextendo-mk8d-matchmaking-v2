package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
	"github.com/redis/go-redis/v9"
)

// redisStatePublisher mirrors live server state into Redis. Matchmaking remains
// authoritative in memory: a temporary Redis outage must never interrupt an
// active race. The mirror gives dashboards and future multi-instance workers a
// consistent, expiring view of rooms and presence without storing IP addresses,
// tickets, station URLs, or session keys.
type redisStatePublisher struct {
	client      *redis.Client
	endpoint    *nex.Endpoint
	matchmaking *nex.Matchmaking
	prefix      string
	instance    string
	ttl         time.Duration
	interval    time.Duration
	required    bool

	events  chan redisPresenceEvent
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	healthy atomic.Bool
	dropped atomic.Uint64
	// publishedRooms is owned by the publisher goroutine and tracks which
	// per-room leases this instance must remove after a room closes.
	publishedRooms map[string]struct{}
}

type redisPresenceEvent struct {
	Kind         string
	PID          uint64
	ConnectionID uint32
	At           time.Time
}

type redisRoomRecord struct {
	ID                uint32   `json:"id"`
	GameMode          uint32   `json:"gameMode"`
	OwnerPID          uint64   `json:"ownerPid"`
	HostPID           uint64   `json:"hostPid"`
	MaxParticipants   uint32   `json:"maxParticipants"`
	OpenParticipation bool     `json:"openParticipation"`
	Phase             string   `json:"phase"`
	Epoch             uint64   `json:"epoch"`
	Participants      []uint64 `json:"participants"`
	Reconnecting      []uint64 `json:"reconnecting,omitempty"`
	CreatedAt         string   `json:"createdAt"`
	LastActivity      string   `json:"lastActivity"`
}

type redisRoomSnapshot struct {
	SchemaVersion int               `json:"schemaVersion"`
	Instance      string            `json:"instance"`
	UpdatedAt     string            `json:"updatedAt"`
	Rooms         []redisRoomRecord `json:"rooms"`
}

type redisPresenceRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	Instance      string `json:"instance"`
	PID           uint64 `json:"pid"`
	Connections   int    `json:"connections"`
	RoomID        uint32 `json:"roomId,omitempty"`
	UpdatedAt     string `json:"updatedAt"`
}

type redisServiceRecord struct {
	SchemaVersion int    `json:"schemaVersion"`
	Instance      string `json:"instance"`
	Status        string `json:"status"`
	Connected     int    `json:"connected"`
	Rooms         int    `json:"rooms"`
	DroppedEvents uint64 `json:"droppedEvents"`
	UpdatedAt     string `json:"updatedAt"`
}

func newRedisStatePublisher(endpoint *nex.Endpoint, matchmaking *nex.Matchmaking) (*redisStatePublisher, error) {
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		return nil, nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	instance := strings.TrimSpace(os.Getenv("NEXTENDO_INSTANCE_ID"))
	if instance == "" {
		instance, _ = os.Hostname()
	}
	if instance == "" {
		instance = "mk8d"
	}
	p := &redisStatePublisher{
		client:         redis.NewClient(opts),
		endpoint:       endpoint,
		matchmaking:    matchmaking,
		prefix:         redisPrefix(envOr("REDIS_KEY_PREFIX", "nextendo:mk8d")),
		instance:       redisKeyPart(instance),
		ttl:            time.Duration(envOrInt("REDIS_STATE_TTL_SECONDS", 30)) * time.Second,
		interval:       time.Duration(envOrInt("REDIS_SYNC_INTERVAL_SECONDS", 5)) * time.Second,
		required:       envBool("REDIS_REQUIRED"),
		events:         make(chan redisPresenceEvent, 256),
		publishedRooms: make(map[string]struct{}),
	}
	if p.ttl < 10*time.Second {
		p.ttl = 10 * time.Second
	}
	if p.interval < time.Second {
		p.interval = time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	err = p.client.Ping(ctx).Err()
	cancel()
	if err != nil {
		if p.required {
			_ = p.client.Close()
			return nil, fmt.Errorf("required Redis unavailable: %w", err)
		}
		fmt.Printf("[Redis] unavailable at startup; server will continue and retry: %v\n", err)
	} else {
		p.healthy.Store(true)
		fmt.Printf("[Redis] connected (prefix=%s instance=%s)\n", p.prefix, p.instance)
	}
	return p, nil
}

func (p *redisStatePublisher) Start() {
	if p == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.wg.Add(1)
	go p.run(ctx)
}

func (p *redisStatePublisher) Close() {
	if p == nil {
		return
	}
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	keys := []string{p.serviceKey(), p.roomsKey()}
	members := make([]any, 0, len(p.publishedRooms))
	for member := range p.publishedRooms {
		keys = append(keys, p.roomKey(member))
		members = append(members, member)
	}
	pipe := p.client.Pipeline()
	pipe.Del(ctx, keys...)
	if len(members) != 0 {
		pipe.ZRem(ctx, p.roomIndexKey(), members...)
	}
	_, _ = pipe.Exec(ctx)
	cancel()
	_ = p.client.Close()
}

func (p *redisStatePublisher) ConnectionChanged(kind string, c *nex.Connection) {
	if p == nil || c == nil || c.PID == 0 {
		return
	}
	event := redisPresenceEvent{Kind: kind, PID: c.PID, ConnectionID: c.ID, At: time.Now().UTC()}
	select {
	case p.events <- event:
	default:
		p.dropped.Add(1)
	}
}

func (p *redisStatePublisher) run(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	p.publish(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-p.events:
			p.publishEvent(ctx, event)
			p.publish(ctx)
		case <-ticker.C:
			p.publish(ctx)
		}
	}
}

func (p *redisStatePublisher) publish(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	now := time.Now().UTC()
	rooms, roomByPID := buildRedisRoomSnapshot(p.instance, now, p.matchmaking.SessionInfos())
	connections := p.endpoint.SnapshotConnections()
	counts := make(map[uint64]int)
	for _, c := range connections {
		if c.PID != 0 {
			counts[c.PID]++
		}
	}

	pipe := p.client.Pipeline()
	pipe.Set(ctx, p.roomsKey(), mustJSON(rooms), p.ttl)
	currentRooms := make(map[string]struct{}, len(rooms.Rooms))
	for _, room := range rooms.Rooms {
		member := p.roomMember(room.ID)
		currentRooms[member] = struct{}{}
		pipe.Set(ctx, p.roomKey(member), mustJSON(room), p.ttl)
		pipe.ZAdd(ctx, p.roomIndexKey(), redis.Z{Score: float64(now.UnixMilli()), Member: member})
	}
	for member := range p.publishedRooms {
		if _, stillLive := currentRooms[member]; !stillLive {
			pipe.Del(ctx, p.roomKey(member))
			pipe.ZRem(ctx, p.roomIndexKey(), member)
		}
	}
	// Sorted-set members do not have individual TTLs, so every instance safely
	// prunes globally stale leases. The room detail keys themselves also expire.
	pipe.ZRemRangeByScore(ctx, p.roomIndexKey(), "-inf", strconv.FormatInt(now.Add(-p.ttl).UnixMilli(), 10))
	for pid, count := range counts {
		presence := redisPresenceRecord{
			SchemaVersion: 1, Instance: p.instance, PID: pid, Connections: count,
			RoomID: roomByPID[pid], UpdatedAt: now.Format(time.RFC3339Nano),
		}
		pipe.Set(ctx, p.presenceKey(pid), mustJSON(presence), p.ttl)
	}
	service := redisServiceRecord{
		SchemaVersion: 1, Instance: p.instance, Status: "online", Connected: len(counts),
		Rooms: len(rooms.Rooms), DroppedEvents: p.dropped.Load(), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	pipe.Set(ctx, p.serviceKey(), mustJSON(service), p.ttl)
	_, err := pipe.Exec(ctx)
	if err == nil {
		p.publishedRooms = currentRooms
	}
	p.setHealth(err)
}

func (p *redisStatePublisher) publishEvent(parent context.Context, event redisPresenceEvent) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	values := map[string]any{
		"schemaVersion": 1,
		"instance":      p.instance,
		"kind":          event.Kind,
		"pid":           strconv.FormatUint(event.PID, 10),
		"connectionId":  strconv.FormatUint(uint64(event.ConnectionID), 10),
		"at":            event.At.Format(time.RFC3339Nano),
	}
	err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.eventsKey(), MaxLen: 10000, Approx: true, Values: values,
	}).Err()
	if event.Kind == "disconnected" {
		if delErr := p.client.Del(ctx, p.presenceKey(event.PID)).Err(); err == nil {
			err = delErr
		}
	}
	p.setHealth(err)
}

func (p *redisStatePublisher) setHealth(err error) {
	wasHealthy := p.healthy.Swap(err == nil)
	if err != nil && wasHealthy {
		fmt.Printf("[Redis] state publishing degraded: %v\n", err)
	}
	if err == nil && !wasHealthy {
		fmt.Println("[Redis] state publishing recovered")
	}
}

func buildRedisRoomSnapshot(instance string, now time.Time, sessions []nex.MatchmakingSessionInfo) (redisRoomSnapshot, map[uint64]uint32) {
	snapshot := redisRoomSnapshot{
		SchemaVersion: 1, Instance: instance, UpdatedAt: now.Format(time.RFC3339Nano),
		Rooms: make([]redisRoomRecord, 0, len(sessions)),
	}
	roomByPID := make(map[uint64]uint32)
	for _, s := range sessions {
		record := redisRoomRecord{
			ID: s.ID, GameMode: s.GameMode, OwnerPID: s.OwnerPID, HostPID: s.HostPID,
			MaxParticipants: s.MaxParticipants, OpenParticipation: s.OpenParticipation,
			Phase: s.Phase.String(), Epoch: s.Epoch,
			Participants: append([]uint64(nil), s.Participants...),
			Reconnecting: append([]uint64(nil), s.Reconnecting...),
			CreatedAt:    s.CreatedAt.UTC().Format(time.RFC3339Nano),
			LastActivity: s.LastActivity.UTC().Format(time.RFC3339Nano),
		}
		snapshot.Rooms = append(snapshot.Rooms, record)
		for _, pid := range s.Participants {
			roomByPID[pid] = s.ID
		}
	}
	return snapshot, roomByPID
}

func (p *redisStatePublisher) status() (enabled, healthy bool, instance string) {
	if p == nil {
		return false, false, "local"
	}
	return true, p.healthy.Load(), p.instance
}

func (p *redisStatePublisher) roomsKey() string   { return p.prefix + ":rooms:" + p.instance }
func (p *redisStatePublisher) eventsKey() string  { return p.prefix + ":events" }
func (p *redisStatePublisher) serviceKey() string { return p.prefix + ":service:" + p.instance }
func (p *redisStatePublisher) presenceKey(pid uint64) string {
	return p.prefix + ":presence:" + p.instance + ":" + strconv.FormatUint(pid, 10)
}
func (p *redisStatePublisher) roomIndexKey() string { return p.prefix + ":room-index" }
func (p *redisStatePublisher) roomMember(gid uint32) string {
	return p.instance + ":" + strconv.FormatUint(uint64(gid), 10)
}
func (p *redisStatePublisher) roomKey(member string) string {
	return p.prefix + ":room:" + redisKeyPart(member)
}

func redisPrefix(value string) string {
	parts := strings.Split(strings.Trim(value, ":"), ":")
	for i := range parts {
		parts[i] = redisKeyPart(parts[i])
	}
	return strings.Join(parts, ":")
}

func redisKeyPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, value)
	return strings.Trim(value, "-")
}

func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func mustJSON(value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		panic(errors.New("internal Redis state is not JSON serializable: " + err.Error()))
	}
	return string(b)
}
