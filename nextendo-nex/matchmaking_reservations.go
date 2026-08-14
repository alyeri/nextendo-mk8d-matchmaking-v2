package nex

import "time"

const defaultJoinReservationTTL = 8 * time.Second

type joinReservation struct {
	expires time.Time
}

// SetJoinReservationTTL changes how long an uncommitted seat remains reserved.
// Values at or below zero restore the conservative default.
func (m *Matchmaking) SetJoinReservationTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultJoinReservationTTL
	}
	m.mu.Lock()
	m.reservationTTL = ttl
	m.mu.Unlock()
}

// ReserveJoin atomically reserves one seat for pid. Reservations are internal
// control-plane state and are deliberately not included in NumParticipants.
func (m *Matchmaking) ReserveJoin(gid uint32, pid uint64) bool {
	if pid == 0 {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.gatherings[gid]
	return g != nil && m.reserveJoinLocked(g, pid)
}

// CancelJoinReservation releases a pending seat without affecting a committed
// participant. It is safe to call after a failed or abandoned join flow.
func (m *Matchmaking) CancelJoinReservation(gid uint32, pid uint64) {
	m.mu.Lock()
	if g := m.gatherings[gid]; g != nil {
		delete(g.reservations, pid)
	}
	m.mu.Unlock()
}

func (m *Matchmaking) expireJoinReservationsLocked(g *gathering, now time.Time) {
	for pid, reservation := range g.reservations {
		if !now.Before(reservation.expires) {
			delete(g.reservations, pid)
		}
	}
}

func (m *Matchmaking) occupiedSeatsLocked(g *gathering, now time.Time) int {
	m.expireJoinReservationsLocked(g, now)
	return len(g.participants) + len(g.reservations)
}

func (m *Matchmaking) reserveJoinLocked(g *gathering, pid uint64) bool {
	if containsPID(g.participants, pid) {
		delete(g.reservations, pid)
		return true
	}
	now := m.now()
	m.expireJoinReservationsLocked(g, now)
	if _, exists := g.reservations[pid]; exists {
		g.reservations[pid] = joinReservation{expires: now.Add(m.reservationTTL)}
		return true
	}
	if g.session.MaxParticipants > 0 && len(g.participants)+len(g.reservations) >= int(g.session.MaxParticipants) {
		return false
	}
	g.reservations[pid] = joinReservation{expires: now.Add(m.reservationTTL)}
	return true
}

func (m *Matchmaking) commitReservedJoinLocked(g *gathering, pid uint64) bool {
	if containsPID(g.participants, pid) {
		delete(g.reservations, pid)
		return false
	}
	reservation, exists := g.reservations[pid]
	if !exists || !m.now().Before(reservation.expires) {
		delete(g.reservations, pid)
		return false
	}
	delete(g.reservations, pid)
	g.participants = append(g.participants, pid)
	return true
}
