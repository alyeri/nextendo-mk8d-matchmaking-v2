package main

import (
	"crypto/sha256"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

type rmcDeduplicator struct {
	mu      sync.Mutex
	entries map[rmcDedupKey]rmcDedupEntry
	ttl     time.Duration
	now     func() time.Time
	calls   uint64
}

type rmcDedupKey struct {
	connectionID uint32
	protocol     uint16
	method       uint32
	callID       uint32
	bodyHash     [32]byte
}

type rmcDedupEntry struct {
	response *nex.RMCMessage
	expires  time.Time
}

func newRMCDeduplicator(ttl time.Duration) *rmcDeduplicator {
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	return &rmcDeduplicator{entries: map[rmcDedupKey]rmcDedupEntry{}, ttl: ttl, now: time.Now}
}

func (d *rmcDeduplicator) wrap(next nex.RMCHandler) nex.RMCHandler {
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if d == nil || conn == nil || req == nil || !deduplicateMatchmaking(req) {
			return next(conn, req)
		}
		key := rmcDedupKey{
			connectionID: conn.ID, protocol: req.Protocol, method: req.Method,
			callID: req.CallID, bodyHash: sha256.Sum256(req.Body),
		}
		now := d.now()
		d.mu.Lock()
		d.calls++
		if d.calls%1024 == 0 {
			for candidate, entry := range d.entries {
				if !now.Before(entry.expires) {
					delete(d.entries, candidate)
				}
			}
		}
		if cached, ok := d.entries[key]; ok && now.Before(cached.expires) {
			response := cloneRMC(cached.response)
			d.mu.Unlock()
			return response
		}
		d.mu.Unlock()

		response := next(conn, req)
		if response != nil {
			d.mu.Lock()
			d.entries[key] = rmcDedupEntry{response: cloneRMC(response), expires: now.Add(d.ttl)}
			d.mu.Unlock()
		}
		return response
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
			nex.MethodModifyCurrentGameAttribute:
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
