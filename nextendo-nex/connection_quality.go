package nex

import (
	"sync"
	"time"
)

// ConnectionQuality is server-owned connectivity metadata. It never enters a
// NEX response, so matchmaking policy can evolve without changing the game
// protocol or requiring a client patch.
type ConnectionQuality struct {
	PID             uint64
	ObservedAt      time.Time
	NATMapping      int
	NATFiltering    int
	HasNATMapping   bool
	HasNATFiltering bool
	DirectReady     bool
	ReplaceSeen     bool
}

// ConnectionQualityRegistry stores the most recent connectivity observation
// for each authenticated player. A later Redis adapter can implement the same
// lookup contract when matchmaking is split across processes.
type ConnectionQualityRegistry struct {
	mu      sync.RWMutex
	players map[uint64]ConnectionQuality
	now     func() time.Time
}

func NewConnectionQualityRegistry() *ConnectionQualityRegistry {
	return &ConnectionQualityRegistry{
		players: make(map[uint64]ConnectionQuality),
		now:     time.Now,
	}
}

// Observe records only fields actually reported by the client. Unknown NAT
// values remain unknown instead of being guessed.
func (r *ConnectionQualityRegistry) Observe(pid uint64, stations []*StationURL, replaceSeen bool) {
	if r == nil || pid == 0 {
		return
	}
	r.mu.Lock()
	q := r.players[pid]
	q.PID = pid
	q.ObservedAt = r.now()
	q.ReplaceSeen = q.ReplaceSeen || replaceSeen
	for _, station := range stations {
		if station == nil {
			continue
		}
		if station.Has("natm") {
			q.NATMapping = station.GetInt("natm")
			q.HasNATMapping = true
		}
		if station.Has("natf") {
			q.NATFiltering = station.GetInt("natf")
			q.HasNATFiltering = true
		}
		if station.Get("address") != "" && station.GetInt("port") > 0 && station.GetInt("RVCID") > 0 {
			q.DirectReady = true
		}
	}
	r.players[pid] = q
	r.mu.Unlock()
}

func (r *ConnectionQualityRegistry) Lookup(pid uint64) (ConnectionQuality, bool) {
	if r == nil {
		return ConnectionQuality{}, false
	}
	r.mu.RLock()
	q, ok := r.players[pid]
	r.mu.RUnlock()
	return q, ok
}

// PairScore returns a small, bounded preference for a room whose host has a
// fresh and complete direct-connect observation. It intentionally does not
// assign meaning to undocumented natm/natf numeric values.
func (r *ConnectionQualityRegistry) PairScore(_ uint64, hostPID uint64) int64 {
	return r.HostScore(hostPID)
}

// HostScore ranks only facts the server has actually observed. It deliberately
// avoids guessing undocumented NAT enum semantics or client FPS.
func (r *ConnectionQualityRegistry) HostScore(hostPID uint64) int64 {
	q, ok := r.Lookup(hostPID)
	if !ok {
		return 0
	}
	age := r.now().Sub(q.ObservedAt)
	if age < 0 {
		age = 0
	}
	if age > 2*time.Minute {
		return 0
	}
	score := int64(20)
	if q.DirectReady {
		score += 50
	}
	if q.ReplaceSeen {
		score += 20
	}
	if q.HasNATMapping && q.HasNATFiltering {
		score += 5
	}
	// Freshness contributes 0..5 and never dominates room occupancy.
	score += int64((2*time.Minute - age) * 5 / (2 * time.Minute))
	return score
}

func (r *ConnectionQualityRegistry) Count() int {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	n := len(r.players)
	r.mu.RUnlock()
	return n
}
