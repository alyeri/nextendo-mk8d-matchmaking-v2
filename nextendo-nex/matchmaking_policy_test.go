package nex

import (
	"testing"
	"time"
)

func TestCompatibleSessionsUsesConfiguredAttributePrefix(t *testing.T) {
	a := &MatchmakeSession{GameMode: 1, Attribs: []uint32{10, 20, 30}}
	b := &MatchmakeSession{GameMode: 1, Attribs: []uint32{10, 99, 30}}
	if !compatibleSessions(a, b, 0) {
		t.Fatal("zero attributes must preserve game-mode-only matching")
	}
	if !compatibleSessions(a, b, 1) {
		t.Fatal("first attribute is equal")
	}
	if compatibleSessions(a, b, 2) {
		t.Fatal("second attribute differs")
	}
	b.GameMode = 3
	if compatibleSessions(a, b, 0) {
		t.Fatal("different game modes must never match")
	}
}

func TestJoinReservationPreventsOverbooking(t *testing.T) {
	now := time.Unix(100, 0)
	m := NewMatchmakingWithOptions(MatchmakingOptions{
		ReservationTTL: time.Minute,
		Now:            func() time.Time { return now },
	})
	g := &gathering{
		session:      &MatchmakeSession{Gathering: Gathering{MaxParticipants: 2}, OpenParticipation: true},
		participants: []uint64{1}, reservations: map[uint64]joinReservation{},
		createdAt: now, lastActivity: now,
	}
	if !m.reserveJoinLocked(g, 2, true) {
		t.Fatal("second seat should be reservable")
	}
	if m.reserveJoinLocked(g, 3, true) {
		t.Fatal("reservation must count against capacity")
	}
	if !m.commitJoinLocked(g, 2) {
		t.Fatal("valid reservation should commit")
	}
	if len(g.participants) != 2 || g.phase != SessionReady {
		t.Fatalf("unexpected committed state: participants=%v phase=%v", g.participants, g.phase)
	}
}

func TestReconnectGraceKeepsThenRemovesSession(t *testing.T) {
	m := NewMatchmakingWithOptions(MatchmakingOptions{DisconnectGrace: 30 * time.Millisecond})
	now := m.options.Now()
	m.gatherings[1] = &gathering{
		session:      &MatchmakeSession{Gathering: Gathering{ID: 1, OwnerPID: 7, MaxParticipants: 12}},
		participants: []uint64{7}, reservations: map[uint64]joinReservation{},
		createdAt: now, lastActivity: now,
	}
	m.MarkPlayerDisconnected(7)
	if len(m.SessionInfos()) != 1 {
		t.Fatal("session was removed before grace expired")
	}
	time.Sleep(80 * time.Millisecond)
	if len(m.SessionInfos()) != 0 {
		t.Fatal("expired disconnect lease did not remove owner session")
	}
}

func TestReconnectCancelsEviction(t *testing.T) {
	m := NewMatchmakingWithOptions(MatchmakingOptions{DisconnectGrace: 30 * time.Millisecond})
	now := m.options.Now()
	m.gatherings[1] = &gathering{
		session:      &MatchmakeSession{Gathering: Gathering{ID: 1, OwnerPID: 7, MaxParticipants: 12}},
		participants: []uint64{7}, reservations: map[uint64]joinReservation{},
		createdAt: now, lastActivity: now,
	}
	m.MarkPlayerDisconnected(7)
	m.markPlayerActive(7)
	time.Sleep(80 * time.Millisecond)
	if len(m.SessionInfos()) != 1 {
		t.Fatal("reconnected player was evicted")
	}
}

func TestExpiredHostMigratesToLiveParticipant(t *testing.T) {
	m := NewMatchmakingWithOptions(MatchmakingOptions{
		DisconnectGrace: 30 * time.Millisecond,
		ConnectionIDForPID: func(pid uint64) uint32 {
			if pid == 8 {
				return 88
			}
			return 0
		},
	})
	now := m.options.Now()
	m.gatherings[1] = &gathering{
		session: &MatchmakeSession{Gathering: Gathering{
			ID: 1, OwnerPID: 7, HostPID: 7, MaxParticipants: 12,
		}},
		participants: []uint64{7, 8}, hostConnID: 77,
		reservations: map[uint64]joinReservation{}, createdAt: now, lastActivity: now,
	}
	m.MarkPlayerDisconnected(7)
	time.Sleep(80 * time.Millisecond)

	m.mu.Lock()
	g := m.gatherings[1]
	if g == nil || g.session.OwnerPID != 8 || g.session.HostPID != 8 || g.hostConnID != 88 {
		t.Fatalf("host was not migrated: %+v", g)
	}
	if len(g.participants) != 1 || g.participants[0] != 8 {
		t.Fatalf("participants after migration = %v", g.participants)
	}
	m.mu.Unlock()
}

func TestSessionPhaseTransitionsRejectInvalidJump(t *testing.T) {
	if validPhaseTransition(SessionSearching, SessionRacing) {
		t.Fatal("searching must not jump directly to racing")
	}
	if !validPhaseTransition(SessionReady, SessionRacing) || !validPhaseTransition(SessionRacing, SessionResults) || !validPhaseTransition(SessionResults, SessionSearching) {
		t.Fatal("normal race lifecycle transition rejected")
	}
}

func TestAdaptiveCompatibilityRelaxesOverTime(t *testing.T) {
	now := time.Unix(100, 0)
	m := NewMatchmakingWithOptions(MatchmakingOptions{
		CompatibilityAttributes: 3,
		AdaptiveRelaxAfter:      5 * time.Second,
		Now:                     func() time.Time { return now },
	})
	g := &gathering{createdAt: now}
	if got := m.compatibilityAttributesLocked(g, now); got != 3 {
		t.Fatalf("initial compatibility attributes = %d", got)
	}
	if got := m.compatibilityAttributesLocked(g, now.Add(11*time.Second)); got != 1 {
		t.Fatalf("relaxed compatibility attributes = %d", got)
	}
}

func TestBestHostUsesObservedScoreAndLiveParticipant(t *testing.T) {
	scores := map[uint64]int64{1: 10, 2: 90, 3: 50}
	m := NewMatchmakingWithOptions(MatchmakingOptions{HostScore: func(pid uint64) int64 { return scores[pid] }})
	g := &gathering{participants: []uint64{1, 2, 3}}
	if got := m.bestHostLocked(g); got != 2 {
		t.Fatalf("best host = %d", got)
	}
	m.disconnected[2] = disconnectLease{}
	if got := m.bestHostLocked(g); got != 3 {
		t.Fatalf("best live host = %d", got)
	}
}

func TestReservePartyIsAtomic(t *testing.T) {
	now := time.Unix(100, 0)
	m := NewMatchmakingWithOptions(MatchmakingOptions{ReservationTTL: time.Minute, Now: func() time.Time { return now }})
	m.gatherings[1] = &gathering{
		session:      &MatchmakeSession{Gathering: Gathering{ID: 1, MaxParticipants: 3}, OpenParticipation: true},
		participants: []uint64{1}, reservations: map[uint64]joinReservation{}, createdAt: now, lastActivity: now,
	}
	if !m.ReserveParty(1, []uint64{2, 3}) || len(m.gatherings[1].reservations) != 2 {
		t.Fatal("two-seat party was not reserved atomically")
	}
	if m.ReserveParty(1, []uint64{4}) || len(m.gatherings[1].reservations) != 2 {
		t.Fatal("over-capacity party partially changed reservations")
	}
}

func TestPruneExpiredSessionsUsesPhaseTTL(t *testing.T) {
	now := time.Unix(100, 0)
	m := NewMatchmakingWithOptions(MatchmakingOptions{
		SearchIdleTTL: 10 * time.Second, SessionIdleTTL: time.Minute,
		Now: func() time.Time { return now },
	})
	m.gatherings[1] = &gathering{
		session: &MatchmakeSession{Gathering: Gathering{ID: 1}}, participants: []uint64{1},
		phase: SessionSearching, createdAt: now.Add(-time.Minute), lastActivity: now.Add(-11 * time.Second), reservations: map[uint64]joinReservation{},
	}
	if removed := m.PruneExpiredSessions(); removed != 1 || len(m.gatherings) != 0 {
		t.Fatalf("removed=%d remaining=%d", removed, len(m.gatherings))
	}
}

func TestIntermissionUsesLongerGrace(t *testing.T) {
	m := NewMatchmakingWithOptions(MatchmakingOptions{DisconnectGrace: 5 * time.Second, IntermissionGrace: 20 * time.Second})
	m.gatherings[1] = &gathering{
		session: &MatchmakeSession{Gathering: Gathering{ID: 1}}, participants: []uint64{1},
		phase: SessionResults, reservations: map[uint64]joinReservation{},
	}
	m.mu.Lock()
	grace := m.disconnectGraceLocked(1)
	m.mu.Unlock()
	if grace != 20*time.Second {
		t.Fatalf("intermission grace = %s", grace)
	}
}
