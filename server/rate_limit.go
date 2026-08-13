package main

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

// requestLimiter is intentionally local to one server instance. It protects
// expensive control-plane calls without putting Redis or any network round trip
// in the latency-sensitive RMC path.
type requestLimiter struct {
	mu      sync.Mutex
	entries map[string]*limitEntry
	now     func() time.Time
	calls   uint64
}

type limitEntry struct {
	windowStart time.Time
	count       int
	strikes     int
	blockedTill time.Time
	lastSeen    time.Time
}

type limitPolicy struct {
	name      string
	limit     int
	window    time.Duration
	baseBlock time.Duration
	maxBlock  time.Duration
}

type limitDecision struct {
	allowed  bool
	retryIn  time.Duration
	newBlock bool
}

func newRequestLimiter() *requestLimiter {
	return &requestLimiter{entries: map[string]*limitEntry{}, now: time.Now}
}

func (l *requestLimiter) allow(key string, policy limitPolicy) limitDecision {
	if l == nil || policy.limit <= 0 || key == "" {
		return limitDecision{allowed: true}
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	if l.calls%1024 == 0 {
		l.pruneLocked(now)
	}
	e := l.entries[key]
	if e == nil {
		e = &limitEntry{windowStart: now}
		l.entries[key] = e
	}
	if now.Before(e.blockedTill) {
		e.lastSeen = now
		return limitDecision{retryIn: e.blockedTill.Sub(now)}
	}
	if now.Sub(e.lastSeen) > 10*time.Minute {
		e.strikes = 0
	}
	if e.windowStart.IsZero() || now.Sub(e.windowStart) >= policy.window {
		e.windowStart = now
		e.count = 0
	}
	e.lastSeen = now
	e.count++
	if e.count <= policy.limit {
		return limitDecision{allowed: true}
	}
	e.strikes++
	block := policy.baseBlock
	for i := 1; i < e.strikes && block < policy.maxBlock; i++ {
		block *= 2
	}
	if block > policy.maxBlock {
		block = policy.maxBlock
	}
	e.blockedTill = now.Add(block)
	e.windowStart = now
	e.count = 0
	return limitDecision{retryIn: block, newBlock: true}
}

func (l *requestLimiter) pruneLocked(now time.Time) {
	for key, entry := range l.entries {
		if !now.Before(entry.blockedTill) && now.Sub(entry.lastSeen) > time.Hour {
			delete(l.entries, key)
		}
	}
}

func remoteHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func wrapAuthRateLimit(next nex.RMCHandler, limiter *requestLimiter) nex.RMCHandler {
	policy := limitPolicy{
		name: "auth-ip", limit: envOrInt("AUTH_RATE_LIMIT_PER_MINUTE", 30), window: time.Minute,
		baseBlock: time.Duration(envOrInt("AUTH_RATE_BLOCK_SECONDS", 30)) * time.Second,
		maxBlock:  10 * time.Minute,
	}
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		addr := "unknown"
		if conn != nil {
			addr = remoteHost(conn.RemoteAddr)
		}
		decision := limiter.allow(policy.name+":"+addr, policy)
		if !decision.allowed {
			logRateBlock(policy.name, "ip="+addr, decision)
			return nex.NewRMCError(reqSettings(conn, req), req.Protocol, req.CallID, nex.ResultCoreAccessDenied)
		}
		return next(conn, req)
	}
}

func wrapMatchmakingRateLimit(next nex.RMCHandler, limiter *requestLimiter) nex.RMCHandler {
	general := limitPolicy{
		name: "matchmaking", limit: envOrInt("MATCHMAKING_RATE_LIMIT_PER_10_SECONDS", 300), window: 10 * time.Second,
		baseBlock: time.Duration(envOrInt("MATCHMAKING_RATE_BLOCK_SECONDS", 10)) * time.Second,
		maxBlock:  5 * time.Minute,
	}
	expensive := limitPolicy{
		name: "matchmaking-expensive", limit: envOrInt("MATCHMAKING_EXPENSIVE_LIMIT_PER_10_SECONDS", 40), window: 10 * time.Second,
		baseBlock: time.Duration(envOrInt("MATCHMAKING_RATE_BLOCK_SECONDS", 10)) * time.Second,
		maxBlock:  5 * time.Minute,
	}
	return func(conn *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if conn == nil {
			return next(conn, req)
		}
		host := remoteHost(conn.RemoteAddr)
		pid := strconv.FormatUint(conn.PID, 10)
		for _, subject := range []string{"ip:" + host, "pid:" + pid} {
			if decision := limiter.allow(general.name+":"+subject, general); !decision.allowed {
				logRateBlock(general.name, subject, decision)
				return nex.NewRMCError(conn.Settings, req.Protocol, req.CallID, nex.ResultCoreAccessDenied)
			}
		}
		if isExpensiveMatchmaking(req) {
			for _, subject := range []string{"ip:" + host, "pid:" + pid} {
				if decision := limiter.allow(expensive.name+":"+subject, expensive); !decision.allowed {
					logRateBlock(expensive.name, subject, decision)
					return nex.NewRMCError(conn.Settings, req.Protocol, req.CallID, nex.ResultCoreAccessDenied)
				}
			}
		}
		return next(conn, req)
	}
}

func isExpensiveMatchmaking(req *nex.RMCMessage) bool {
	if req == nil || req.Protocol != nex.ProtocolMatchmakeExtension {
		return false
	}
	switch req.Method {
	case nex.MethodCreateMatchmakeSessionWithParam,
		nex.MethodJoinMatchmakeSessionWithParam,
		nex.MethodAutoMatchmakeWithParamPostpone,
		nex.MethodCustomPrivateRoomCreate,
		nex.MethodBrowseNoHolder:
		return true
	default:
		return false
	}
}

func logRateBlock(scope, subject string, decision limitDecision) {
	if decision.newBlock {
		fmt.Printf("[RateLimit] %s %s temporarily blocked for %s\n", scope, subject, decision.retryIn.Round(time.Second))
	}
}

func reqSettings(conn *nex.Connection, req *nex.RMCMessage) *nex.Settings {
	if conn != nil && conn.Settings != nil {
		return conn.Settings
	}
	// Tests may invoke an auth handler without a transport connection.
	return nex.NewSwitchSettings(accessKey, nexVersion)
}
