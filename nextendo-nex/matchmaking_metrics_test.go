package nex

import (
	"testing"
	"time"
)

func TestMatchmakingMetricsTrackCapacityJoinAndReconnect(t *testing.T) {
	m := NewMatchmakingWithOptions(MatchmakingOptions{DisconnectGrace: time.Second})
	g := &gathering{
		session: &MatchmakeSession{
			Gathering:         Gathering{MaxParticipants: 2},
			OpenParticipation: true,
		},
		participants: []uint64{1}, reservations: map[uint64]joinReservation{},
		createdAt: time.Now(), lastActivity: time.Now(),
	}

	if !m.reserveJoinLocked(g, 2, true) || !m.commitJoinLocked(g, 2) {
		t.Fatal("expected the second player to join")
	}
	if m.reserveJoinLocked(g, 3, true) {
		t.Fatal("full gathering accepted a third player")
	}

	m.gatherings[1] = g
	m.MarkPlayerDisconnected(2)
	m.markPlayerActive(2)

	metrics := m.Metrics()
	if metrics.JoinsCommitted != 1 {
		t.Fatalf("joins committed = %d, want 1", metrics.JoinsCommitted)
	}
	if metrics.ReservationsRejected != 1 {
		t.Fatalf("reservations rejected = %d, want 1", metrics.ReservationsRejected)
	}
	if metrics.ReconnectLeases != 1 || metrics.ReconnectRecovered != 1 {
		t.Fatalf("reconnect metrics = leases:%d recovered:%d", metrics.ReconnectLeases, metrics.ReconnectRecovered)
	}
}
