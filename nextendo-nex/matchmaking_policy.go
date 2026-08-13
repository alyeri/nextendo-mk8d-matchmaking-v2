package nex

import (
	"sort"
	"time"
)

// SessionPhase is server-owned lifecycle metadata. It is deliberately kept out
// of the NEX wire structures so older clients continue to receive byte-identical
// MatchmakeSession responses.
type SessionPhase uint8

const (
	SessionSearching SessionPhase = iota
	SessionForming
	SessionReady
	SessionRacing
	SessionResults
	SessionClosing
)

func (p SessionPhase) String() string {
	switch p {
	case SessionSearching:
		return "searching"
	case SessionForming:
		return "forming"
	case SessionReady:
		return "ready"
	case SessionRacing:
		return "racing"
	case SessionResults:
		return "results"
	case SessionClosing:
		return "closing"
	default:
		return "unknown"
	}
}

// MatchmakingOptions controls policy without changing protocol decoding.
// CompatibilityAttributes compares a prefix of MatchmakeSession.Attribs in
// addition to GameMode. Zero preserves the historical game-mode-only behavior.
type MatchmakingOptions struct {
	CompatibilityAttributes int
	ReservationTTL          time.Duration
	DisconnectGrace         time.Duration
	AdaptiveRelaxAfter      time.Duration
	SearchIdleTTL           time.Duration
	SessionIdleTTL          time.Duration
	IntermissionGrace       time.Duration
	// PairQualityScore may add a bounded preference for one compatible room.
	// nil preserves the historical fullness-and-age selection exactly.
	PairQualityScore func(requesterPID, hostPID uint64) int64
	HostScore        func(pid uint64) int64
	// ConnectionIDForPID lets lifecycle cleanup move server-side ownership to a
	// still-connected participant after the old host's grace period expires.
	ConnectionIDForPID         func(pid uint64) uint32
	EnablePreRaceHostSelection bool
	QualityWeight              int64
	Now                        func() time.Time
}

func normalizeMatchmakingOptions(o MatchmakingOptions) MatchmakingOptions {
	if o.CompatibilityAttributes < 0 {
		o.CompatibilityAttributes = 0
	}
	if o.ReservationTTL <= 0 {
		o.ReservationTTL = 8 * time.Second
	}
	if o.DisconnectGrace < 0 {
		o.DisconnectGrace = 0
	}
	if o.AdaptiveRelaxAfter < 0 {
		o.AdaptiveRelaxAfter = 0
	}
	if o.SearchIdleTTL <= 0 {
		o.SearchIdleTTL = 2 * time.Minute
	}
	if o.SessionIdleTTL <= 0 {
		o.SessionIdleTTL = 20 * time.Minute
	}
	if o.IntermissionGrace < 0 {
		o.IntermissionGrace = 0
	}
	if o.QualityWeight < 0 {
		o.QualityWeight = 0
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

type joinReservation struct {
	pid     uint64
	expires time.Time
}

type disconnectLease struct {
	generation uint64
	expires    time.Time
}

func validPhaseTransition(from, to SessionPhase) bool {
	if from == to {
		return true
	}
	switch from {
	case SessionSearching:
		return to == SessionForming || to == SessionReady || to == SessionClosing
	case SessionForming:
		return to == SessionSearching || to == SessionReady || to == SessionClosing
	case SessionReady:
		return to == SessionForming || to == SessionRacing || to == SessionResults || to == SessionClosing
	case SessionRacing:
		return to == SessionResults || to == SessionClosing
	case SessionResults:
		return to == SessionSearching || to == SessionForming || to == SessionReady || to == SessionClosing
	case SessionClosing:
		return false
	default:
		return false
	}
}

func (m *Matchmaking) transitionLocked(g *gathering, to SessionPhase) bool {
	if g == nil || !validPhaseTransition(g.phase, to) {
		return false
	}
	if g.phase != to {
		g.phase = to
		g.epoch++
	}
	return true
}

func compatibleSessions(a, b *MatchmakeSession, attributeCount int) bool {
	if a == nil || b == nil || a.GameMode != b.GameMode {
		return false
	}
	limit := attributeCount
	if limit > len(a.Attribs) {
		limit = len(a.Attribs)
	}
	if limit > len(b.Attribs) {
		return false
	}
	for i := 0; i < limit; i++ {
		if a.Attribs[i] != b.Attribs[i] {
			return false
		}
	}
	return true
}

func (m *Matchmaking) expireReservationsLocked(g *gathering, now time.Time) {
	for pid, r := range g.reservations {
		if !now.Before(r.expires) {
			delete(g.reservations, pid)
		}
	}
}

func (m *Matchmaking) reserveJoinLocked(g *gathering, pid uint64, requireOpen bool) bool {
	if containsPID(g.participants, pid) {
		return true
	}
	if requireOpen && !g.session.OpenParticipation {
		m.metrics.reservationsRejected.Add(1)
		return false
	}
	now := m.options.Now()
	m.expireReservationsLocked(g, now)
	if g.session.MaxParticipants > 0 && len(g.participants)+len(g.reservations) >= int(g.session.MaxParticipants) {
		m.metrics.reservationsRejected.Add(1)
		return false
	}
	g.reservations[pid] = joinReservation{pid: pid, expires: now.Add(m.options.ReservationTTL)}
	return true
}

func (m *Matchmaking) commitJoinLocked(g *gathering, pid uint64) bool {
	if containsPID(g.participants, pid) {
		delete(g.reservations, pid)
		return false
	}
	r, ok := g.reservations[pid]
	if !ok || !m.options.Now().Before(r.expires) {
		delete(g.reservations, pid)
		return false
	}
	delete(g.reservations, pid)
	g.participants = append(g.participants, pid)
	m.metrics.joinsCommitted.Add(1)
	g.session.NumParticipants = uint32(len(g.participants))
	g.lastActivity = m.options.Now()
	g.epoch++
	if len(g.participants) >= int(g.session.MaxParticipants) {
		m.transitionLocked(g, SessionReady)
	} else if len(g.participants) > 1 {
		m.transitionLocked(g, SessionForming)
	}
	return true
}

func (m *Matchmaking) compatibilityAttributesLocked(g *gathering, now time.Time) int {
	count := m.options.CompatibilityAttributes
	if count == 0 || m.options.AdaptiveRelaxAfter <= 0 || g == nil {
		return count
	}
	age := now.Sub(g.createdAt)
	if age <= 0 {
		return count
	}
	relaxed := int(age / m.options.AdaptiveRelaxAfter)
	count -= relaxed
	if count < 0 {
		count = 0
	}
	return count
}

func (m *Matchmaking) bestHostLocked(g *gathering) uint64 {
	if g == nil || len(g.participants) == 0 {
		return 0
	}
	best := uint64(0)
	bestScore := int64(-1)
	for _, pid := range g.participants {
		if _, disconnected := m.disconnected[pid]; disconnected {
			continue
		}
		score := int64(0)
		if m.options.HostScore != nil {
			score = m.options.HostScore(pid)
		}
		if score > bestScore {
			best, bestScore = pid, score
		}
	}
	if best == 0 {
		return g.participants[0]
	}
	return best
}

func (m *Matchmaking) setHostLocked(g *gathering, pid uint64, transferOwnership bool) bool {
	if g == nil || g.session == nil || pid == 0 || !containsPID(g.participants, pid) {
		return false
	}
	if transferOwnership {
		g.session.Gathering.OwnerPID = pid
	}
	g.session.HostPID = pid
	g.hostConnID = 0
	if m.options.ConnectionIDForPID != nil {
		g.hostConnID = m.options.ConnectionIDForPID(pid)
	}
	g.epoch++
	g.lastActivity = m.options.Now()
	return true
}

// candidateScore prefers fuller compatible rooms, then older rooms. This
// reduces fragmentation while still allowing long-waiting hosts to fill.
func (m *Matchmaking) candidateScore(g *gathering, requesterPID uint64) int64 {
	age := m.options.Now().Sub(g.createdAt) / time.Second
	if age < 0 {
		age = 0
	}
	if age > 300 {
		age = 300
	}
	score := int64(len(g.participants))*1000 + int64(age)
	if m.options.PairQualityScore != nil && m.options.QualityWeight > 0 {
		quality := m.options.PairQualityScore(requesterPID, g.session.HostPID)
		if quality < 0 {
			quality = 0
		}
		if quality > 100 {
			quality = 100
		}
		score += quality * m.options.QualityWeight
	}
	return score
}

func (m *Matchmaking) markPlayerActive(pid uint64) {
	m.markPlayerConnectionActive(pid, 0)
}

func (m *Matchmaking) markPlayerConnectionActive(pid uint64, connectionID uint32) bool {
	if pid == 0 || (m.options.DisconnectGrace == 0 && m.options.IntermissionGrace == 0) {
		return false
	}
	m.mu.Lock()
	recovered := false
	if _, recovering := m.disconnected[pid]; recovering {
		delete(m.disconnected, pid)
		m.metrics.reconnectRecovered.Add(1)
		recovered = true
	}
	for _, g := range m.gatherings {
		if containsPID(g.participants, pid) {
			g.lastActivity = m.options.Now()
			if g.session != nil && g.session.HostPID == pid && connectionID != 0 && g.hostConnID != connectionID {
				g.hostConnID = connectionID
				g.epoch++
			}
		}
	}
	m.mu.Unlock()
	return recovered
}

// ReserveParty atomically reserves seats for a future client feature that can
// submit a party roster. Current MK8D clients join individually, so this API is
// intentionally not inferred from friend lists or IP addresses.
func (m *Matchmaking) ReserveParty(gid uint32, pids []uint64) bool {
	if len(pids) == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.gatherings[gid]
	if g == nil || g.session == nil || !g.session.OpenParticipation {
		return false
	}
	now := m.options.Now()
	m.expireReservationsLocked(g, now)
	needed := 0
	seen := map[uint64]struct{}{}
	for _, pid := range pids {
		if pid == 0 {
			return false
		}
		if _, duplicate := seen[pid]; duplicate {
			continue
		}
		seen[pid] = struct{}{}
		if !containsPID(g.participants, pid) {
			if _, reserved := g.reservations[pid]; !reserved {
				needed++
			}
		}
	}
	if g.session.MaxParticipants > 0 && len(g.participants)+len(g.reservations)+needed > int(g.session.MaxParticipants) {
		m.metrics.reservationsRejected.Add(1)
		return false
	}
	for pid := range seen {
		if !containsPID(g.participants, pid) {
			g.reservations[pid] = joinReservation{pid: pid, expires: now.Add(m.options.ReservationTTL)}
		}
	}
	return true
}

// PruneExpiredSessions removes only control-plane state. It never touches P2P
// race traffic and uses a much longer TTL once a room has reached ready/racing.
func (m *Matchmaking) PruneExpiredSessions() int {
	now := m.options.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	removed := 0
	for gid, g := range m.gatherings {
		if g == nil || g.session == nil {
			delete(m.gatherings, gid)
			removed++
			continue
		}
		m.expireReservationsLocked(g, now)
		ttl := m.options.SessionIdleTTL
		if g.phase == SessionSearching || g.phase == SessionForming {
			ttl = m.options.SearchIdleTTL
		}
		if now.Sub(g.lastActivity) < ttl {
			continue
		}
		m.transitionLocked(g, SessionClosing)
		delete(m.gatherings, gid)
		if g.code != "" {
			delete(m.byCode, g.code)
		}
		removed++
	}
	return removed
}

// MarkPlayerDisconnected grants a short reconnection lease. A reconnect with
// the same PID cancels eviction when its next RMC request arrives.
func (m *Matchmaking) MarkPlayerDisconnected(pid uint64) {
	if pid == 0 {
		return
	}
	if m.options.DisconnectGrace == 0 && m.options.IntermissionGrace == 0 {
		m.RemovePlayer(pid)
		return
	}
	m.mu.Lock()
	grace := m.disconnectGraceLocked(pid)
	if grace <= 0 {
		m.removePlayerLocked(pid)
		m.mu.Unlock()
		return
	}
	m.disconnectGeneration++
	generation := m.disconnectGeneration
	m.disconnected[pid] = disconnectLease{
		generation: generation,
		expires:    m.options.Now().Add(grace),
	}
	m.metrics.reconnectLeases.Add(1)
	m.mu.Unlock()
	time.AfterFunc(grace, func() {
		m.notif.forget(pid)
		m.mu.Lock()
		defer m.mu.Unlock()
		lease, ok := m.disconnected[pid]
		if !ok || lease.generation != generation || m.options.Now().Before(lease.expires) {
			return
		}
		delete(m.disconnected, pid)
		m.removePlayerLocked(pid)
		m.metrics.disconnectEvictions.Add(1)
	})
}

func (m *Matchmaking) disconnectGraceLocked(pid uint64) time.Duration {
	grace := m.options.DisconnectGrace
	for _, g := range m.gatherings {
		if g == nil || !containsPID(g.participants, pid) {
			continue
		}
		if (g.phase == SessionReady || g.phase == SessionResults || g.phase == SessionSearching) && m.options.IntermissionGrace > grace {
			grace = m.options.IntermissionGrace
		}
	}
	return grace
}

// MatchmakingSessionInfo exposes server-owned lifecycle state for monitoring
// and future Redis/PostgreSQL adapters without exposing mutable internals.
type MatchmakingSessionInfo struct {
	ID                uint32
	GameMode          uint32
	OwnerPID          uint64
	HostPID           uint64
	MaxParticipants   uint32
	OpenParticipation bool
	Phase             SessionPhase
	Epoch             uint64
	Participants      []uint64
	Reconnecting      []uint64
	CreatedAt         time.Time
	LastActivity      time.Time
}

func (m *Matchmaking) SessionInfos() []MatchmakingSessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]MatchmakingSessionInfo, 0, len(m.gatherings))
	for gid, g := range m.gatherings {
		reconnecting := make([]uint64, 0)
		for _, pid := range g.participants {
			if _, ok := m.disconnected[pid]; ok {
				reconnecting = append(reconnecting, pid)
			}
		}
		out = append(out, MatchmakingSessionInfo{
			ID: gid, GameMode: g.session.GameMode, OwnerPID: g.session.OwnerPID,
			HostPID: g.session.HostPID, MaxParticipants: uint32(g.session.MaxParticipants),
			OpenParticipation: g.session.OpenParticipation, Phase: g.phase, Epoch: g.epoch,
			Participants: append([]uint64(nil), g.participants...),
			Reconnecting: reconnecting,
			CreatedAt:    g.createdAt, LastActivity: g.lastActivity,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
