package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

func TestBuildRedisRoomSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)
	created := now.Add(-time.Minute)
	snapshot, roomByPID := buildRedisRoomSnapshot("node-1", now, []nex.MatchmakingSessionInfo{{
		ID: 7, GameMode: 1, OwnerPID: 1800000001, HostPID: 1800000001,
		MaxParticipants: 12, OpenParticipation: true, Phase: nex.SessionForming, Epoch: 3,
		Participants: []uint64{1800000001, 1800000002}, CreatedAt: created, LastActivity: now,
	}})
	if len(snapshot.Rooms) != 1 || snapshot.Rooms[0].Phase != "forming" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if roomByPID[1800000002] != 7 {
		t.Fatalf("participant room = %d", roomByPID[1800000002])
	}
	if snapshot.Rooms[0].OwnerPID != 1800000001 || snapshot.Rooms[0].MaxParticipants != 12 {
		t.Fatalf("room lifecycle fields missing: %+v", snapshot.Rooms[0])
	}
	encoded := mustJSON(snapshot)
	if strings.Contains(encoded, "address") || strings.Contains(encoded, "sessionKey") {
		t.Fatalf("snapshot contains sensitive network or session fields: %s", encoded)
	}
	var decoded redisRoomSnapshot
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("snapshot is invalid JSON: %v", err)
	}
}

func TestRedisKeySanitization(t *testing.T) {
	if got := redisPrefix(" nextendo:mk8 d:prod/1 "); got != "nextendo:mk8-d:prod-1" {
		t.Fatalf("redisPrefix = %q", got)
	}
}

func TestRedisDisabledWithoutURL(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	publisher, err := newRedisStatePublisher(nil, nil)
	if err != nil || publisher != nil {
		t.Fatalf("disabled Redis = (%v, %v), want (nil, nil)", publisher, err)
	}
}

func TestRedisGlobalRoomIndexKeysAreInstanceScoped(t *testing.T) {
	publisher := &redisStatePublisher{prefix: "nextendo:mk8d", instance: "node-1"}
	member := publisher.roomMember(7)
	if member != "node-1:7" || publisher.roomIndexKey() != "nextendo:mk8d:room-index" {
		t.Fatalf("member=%q index=%q", member, publisher.roomIndexKey())
	}
	if got := publisher.roomKey(member); got != "nextendo:mk8d:room:node-1-7" {
		t.Fatalf("room key = %q", got)
	}
}
