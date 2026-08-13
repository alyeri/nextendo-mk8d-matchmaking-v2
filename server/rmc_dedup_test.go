package main

import (
	"testing"
	"time"

	nex "github.com/NextendoNetwork/nextendo-nex"
)

func TestRMCDeduplicatorReusesMutationResponse(t *testing.T) {
	settings := nex.NewSwitchSettings("test", 40000)
	endpoint := nex.NewEndpoint(settings)
	conn := nex.NewConnection(endpoint, "127.0.0.1:1234", func([]byte) {})
	conn.ID = 9
	request := nex.NewRMCRequest(settings, nex.ProtocolMatchmakeExtension, nex.MethodAutoMatchmakeWithParamPostpone, 77, []byte{1, 2, 3})
	calls := 0
	dedup := newRMCDeduplicator(time.Minute)
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
	third := handler(conn, request)
	if third.Body[0] != 1 {
		t.Fatal("cached response body was aliased")
	}
}

func TestRMCDeduplicatorDoesNotCacheReadOnlySessionURLs(t *testing.T) {
	request := &nex.RMCMessage{Protocol: nex.ProtocolMatchMaking, Method: nex.MethodGetSessionURLs}
	if deduplicateMatchmaking(request) {
		t.Fatal("asynchronous GetSessionURLs must not be deduplicated")
	}
}
