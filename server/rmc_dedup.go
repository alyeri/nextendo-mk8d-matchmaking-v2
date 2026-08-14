package main

import (
	"crypto/sha256"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

const defaultRMCDedupEntries = 4096

// rmcDeduplicator makes selected mutating RMC calls idempotent for a bounded
// period. It also coalesces requests that arrive while the first execution is
// still in flight; merely caching completed calls is insufficient when two
// transport retries are decoded concurrently.
type rmcDeduplicator struct {
	mu         sync.Mutex
	entries    map[rmcDedupKey]*rmcDedupEntry
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
	calls      uint64
}

type rmcDedupKey struct {
	connectionID uint32
	pid          uint64
	protocol     uint16
	method       uint32
	callID       uint32
	bodyHash     [32]byte
}

type rmcDedupEntry struct {
	ready    chan struct{}
	response *nex.RMCMessage
	expires  time.Time
}

func newRMCDeduplicator(ttl time.Duration, maxEntries int) *rmcDeduplicator {
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	if maxEntries <= 0 {
		maxEntries = defaultRMCDedupEntries
	}
	return &rmcDeduplicator{
		entries: make(map[rmcDedupKey]*rmcDedupEntry),
		ttl:     ttl, maxEntries: maxEntries, now: time.Now,
	}
}

func (d *rmcDeduplicator) wrap(next nex.RMCHandler) nex.RMCHandler {
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if d == nil || conn == nil || req == nil || !deduplicateMatchmaking(req) {
			return next(conn, req)
		}

		key := rmcDedupKey{
			connectionID: conn.ID,
			pid:          conn.PID,
			protocol:     req.Protocol,
			method:       req.Method,
			callID:       req.CallID,
			bodyHash:     sha256.Sum256(req.Body),
		}

		for {
			now := d.now()
			d.mu.Lock()
			d.calls++
			if d.calls%256 == 0 || len(d.entries) >= d.maxEntries {
				d.pruneLocked(now)
			}

			if entry, ok := d.entries[key]; ok {
				if entry.ready != nil {
					ready := entry.ready
					d.mu.Unlock()
					<-ready
					continue
				}
				if now.Before(entry.expires) {
					response := cloneRMC(entry.response)
					d.mu.Unlock()
					return response
				}
				delete(d.entries, key)
			}
			if len(d.entries) >= d.maxEntries {
				// Preserve a hard memory bound. Under sustained unique-key pressure,
				// execute normally rather than evicting a still-live idempotency key.
				d.mu.Unlock()
				return next(conn, req)
			}

			entry := &rmcDedupEntry{ready: make(chan struct{})}
			d.entries[key] = entry
			d.mu.Unlock()

			return d.execute(next, conn, req, key, entry)
		}
	}
}

func (d *rmcDeduplicator) execute(next nex.RMCHandler, conn *nex.Connection, req *nex.RMCMessage, key rmcDedupKey, entry *rmcDedupEntry) (response *nex.RMCMessage) {
	defer func() {
		d.mu.Lock()
		current, stillCurrent := d.entries[key]
		if stillCurrent && current == entry {
			if response == nil {
				delete(d.entries, key)
			} else {
				entry.response = cloneRMC(response)
				entry.expires = d.now().Add(d.ttl)
			}
			close(entry.ready)
			entry.ready = nil
		}
		d.mu.Unlock()
	}()
	return next(conn, req)
}

func (d *rmcDeduplicator) pruneLocked(now time.Time) {
	for key, entry := range d.entries {
		if entry.ready == nil && !now.Before(entry.expires) {
			delete(d.entries, key)
		}
	}
}

func deduplicateMatchmaking(req *nex.RMCMessage) bool {
	switch req.Protocol {
	case nex.ProtocolMatchmakeExtension:
		switch req.Method {
		case nex.MethodCloseParticipation, nex.MethodOpenParticipation,
			nex.MethodCreateMatchmakeSessionWithParam, nex.MethodJoinMatchmakeSessionWithParam,
			nex.MethodAutoMatchmakeWithParamPostpone, nex.MethodCustomPrivateRoomCreate,
			nex.MethodUpdateMatchmakeSessionPart, nex.MethodUpdateApplicationBuffer,
			nex.MethodModifyCurrentGameAttribute, nex.MethodUpdateNotificationData,
			nex.MethodSSBUArenaCode, nex.MethodTournamentCreate, nex.MethodTournamentDelete,
			nex.MethodTournamentJoin:
			return true
		}
	case nex.ProtocolMatchMaking:
		switch req.Method {
		case nex.MethodUnregisterGathering, nex.MethodUpdateSessionURL,
			nex.MethodUpdateSessionHost, nex.MethodUpdateSessionHostV1,
			nex.MethodMigrateGatheringOwnership, nex.MethodMigrateGatheringOwnershipV1,
			nex.MethodUpdateGatheringOwnership:
			return true
		}
	case nex.ProtocolMatchMakingExt:
		return req.Method == nex.MethodEndParticipation
	}
	return false
}

func cloneRMC(message *nex.RMCMessage) *nex.RMCMessage {
	if message == nil {
		return nil
	}
	copyMessage := *message
	copyMessage.Body = append([]byte(nil), message.Body...)
	return &copyMessage
}
