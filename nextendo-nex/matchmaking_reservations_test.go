package nex

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func reservationTestMatchmaking(max uint16, now func() time.Time) (*Matchmaking, *gathering) {
	m := NewMatchmaking()
	m.now = now
	g := &gathering{
		session:      &MatchmakeSession{Gathering: Gathering{ID: 1, MaxParticipants: max}},
		participants: []uint64{1},
		reservations: make(map[uint64]joinReservation),
	}
	m.gatherings[1] = g
	return m, g
}

func TestJoinReservationCountsAgainstCapacity(t *testing.T) {
	now := time.Unix(100, 0)
	m, g := reservationTestMatchmaking(2, func() time.Time { return now })
	if !m.ReserveJoin(1, 2) {
		t.Fatal("last seat should be reservable")
	}
	if m.ReserveJoin(1, 3) {
		t.Fatal("pending reservation must count against capacity")
	}
	if len(g.participants) != 1 || len(g.reservations) != 1 {
		t.Fatalf("unexpected state participants=%v reservations=%v", g.participants, g.reservations)
	}

	m.mu.Lock()
	committed := m.commitReservedJoinLocked(g, 2)
	m.mu.Unlock()
	if !committed || len(g.participants) != 2 || len(g.reservations) != 0 {
		t.Fatalf("commit failed participants=%v reservations=%v", g.participants, g.reservations)
	}
}

func TestJoinReservationExpires(t *testing.T) {
	now := time.Unix(200, 0)
	m, _ := reservationTestMatchmaking(2, func() time.Time { return now })
	m.SetJoinReservationTTL(time.Second)
	if !m.ReserveJoin(1, 2) {
		t.Fatal("initial reservation failed")
	}
	now = now.Add(2 * time.Second)
	if !m.ReserveJoin(1, 3) {
		t.Fatal("expired reservation did not release capacity")
	}
}

func TestJoinReservationIsAtomicUnderConcurrency(t *testing.T) {
	m, g := reservationTestMatchmaking(2, time.Now)
	const contenders = 64
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		pid := uint64(i + 2)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if m.ReserveJoin(1, pid) {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("reserved seats=%d, want exactly 1", got)
	}
	m.mu.Lock()
	occupied := m.occupiedSeatsLocked(g, time.Now())
	m.mu.Unlock()
	if occupied != 2 {
		t.Fatalf("occupied seats=%d, want 2", occupied)
	}
}

func TestCancelJoinReservationIsIdempotent(t *testing.T) {
	m, g := reservationTestMatchmaking(2, time.Now)
	if !m.ReserveJoin(1, 2) {
		t.Fatal("reservation failed")
	}
	m.CancelJoinReservation(1, 2)
	m.CancelJoinReservation(1, 2)
	if len(g.reservations) != 0 {
		t.Fatalf("reservation was not cancelled: %v", g.reservations)
	}
}
