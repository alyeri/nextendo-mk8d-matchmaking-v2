package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

func dedupTestRequest(settings *nex.Settings, callID uint32, body []byte) *nex.RMCMessage {
	return nex.NewRMCRequest(settings, nex.ProtocolMatchmakeExtension, nex.MethodAutoMatchmakeWithParamPostpone, callID, body)
}

func dedupTestConnection(settings *nex.Settings, id uint32, pid uint64) *nex.Connection {
	endpoint := nex.NewEndpoint(settings)
	conn := nex.NewConnection(endpoint, "127.0.0.1:1234", func([]byte) {})
	conn.ID, conn.PID = id, pid
	return conn
}

func TestRMCDeduplicatorReusesMutationResponse(t *testing.T) {
	settings := nex.NewSwitchSettings("test", 40000)
	conn := dedupTestConnection(settings, 9, 90)
	request := dedupTestRequest(settings, 77, []byte{1, 2, 3})
	calls := 0
	dedup := newRMCDeduplicator(time.Minute, 32)
	handler := dedup.wrap(func(_ *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		calls++
		return nex.NewRMCSuccess(settings, req.Protocol, req.Method, req.CallID, []byte{byte(calls)})
	})

	first := handler(conn, request)
	second := handler(conn, request)
	if calls != 1 || first.Body[0] != 1 || second.Body[0] != 1 {
		t.Fatalf("calls=%d first=%v second=%v", calls, first.Body, second.Body)
	}
	second.Body[0] = 99
	if third := handler(conn, request); third.Body[0] != 1 {
		t.Fatal("cached response body was aliased")
	}
}

func TestRMCDeduplicatorCoalescesConcurrentRetries(t *testing.T) {
	settings := nex.NewSwitchSettings("test", 40000)
	conn := dedupTestConnection(settings, 7, 70)
	request := dedupTestRequest(settings, 10, []byte("same request"))
	dedup := newRMCDeduplicator(time.Minute, 32)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	handler := dedup.wrap(func(_ *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return nex.NewRMCSuccess(settings, req.Protocol, req.Method, req.CallID, []byte("one"))
	})

	const workers = 24
	results := make(chan *nex.RMCMessage, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- handler(conn, request)
		}()
	}
	<-started
	time.Sleep(10 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)

	if got := calls.Load(); got != 1 {
		t.Fatalf("underlying mutation executed %d times, want 1", got)
	}
	for response := range results {
		if string(response.Body) != "one" {
			t.Fatalf("unexpected response: %q", response.Body)
		}
	}
}

func TestRMCDeduplicatorSeparatesIdentityAndPayload(t *testing.T) {
	settings := nex.NewSwitchSettings("test", 40000)
	dedup := newRMCDeduplicator(time.Minute, 32)
	var calls atomic.Int32
	handler := dedup.wrap(func(_ *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		n := calls.Add(1)
		return nex.NewRMCSuccess(settings, req.Protocol, req.Method, req.CallID, []byte{byte(n)})
	})

	a := dedupTestConnection(settings, 1, 100)
	b := dedupTestConnection(settings, 2, 200)
	handler(a, dedupTestRequest(settings, 5, []byte("a")))
	handler(a, dedupTestRequest(settings, 5, []byte("b")))
	handler(b, dedupTestRequest(settings, 5, []byte("a")))
	if got := calls.Load(); got != 3 {
		t.Fatalf("distinct identity/payload requests collapsed: calls=%d", got)
	}
}

func TestRMCDeduplicatorExpiresAndDoesNotCacheNil(t *testing.T) {
	settings := nex.NewSwitchSettings("test", 40000)
	conn := dedupTestConnection(settings, 1, 1)
	request := dedupTestRequest(settings, 1, nil)
	now := time.Unix(100, 0)
	dedup := newRMCDeduplicator(time.Second, 32)
	dedup.now = func() time.Time { return now }
	calls := 0
	handler := dedup.wrap(func(_ *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		calls++
		if calls == 1 {
			return nil
		}
		return nex.NewRMCSuccess(settings, req.Protocol, req.Method, req.CallID, nil)
	})

	if handler(conn, request) != nil {
		t.Fatal("first response should be nil")
	}
	handler(conn, request)
	handler(conn, request)
	if calls != 2 {
		t.Fatalf("nil response was cached or completed response was missed: calls=%d", calls)
	}
	now = now.Add(2 * time.Second)
	handler(conn, request)
	if calls != 3 {
		t.Fatalf("expired response remained cached: calls=%d", calls)
	}
}

func TestRMCDeduplicatorDoesNotCacheReadOnlySessionURLs(t *testing.T) {
	request := &nex.RMCMessage{Protocol: nex.ProtocolMatchMaking, Method: nex.MethodGetSessionURLs}
	if deduplicateMatchmaking(request) {
		t.Fatal("asynchronous GetSessionURLs must not be deduplicated")
	}
}

func TestRMCDeduplicatorEnforcesEntryBound(t *testing.T) {
	settings := nex.NewSwitchSettings("test", 40000)
	conn := dedupTestConnection(settings, 1, 1)
	dedup := newRMCDeduplicator(time.Minute, 2)
	handler := dedup.wrap(func(_ *nex.Connection, req *nex.RMCMessage) *nex.RMCMessage {
		return nex.NewRMCSuccess(settings, req.Protocol, req.Method, req.CallID, nil)
	})
	for callID := uint32(1); callID <= 20; callID++ {
		handler(conn, dedupTestRequest(settings, callID, nil))
	}
	dedup.mu.Lock()
	entries := len(dedup.entries)
	dedup.mu.Unlock()
	if entries > 2 {
		t.Fatalf("entries=%d, want at most 2", entries)
	}
}
