package nex

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONLCommonDataStoreSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.jsonl")
	store, err := NewJSONLCommonDataStore(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{1, 2, 3, 4}
	if err := store.PutCommonData(42, want, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewJSONLCommonDataStore(path)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := reloaded.Latest(42)
	if !ok || !bytes.Equal(record.Data, want) {
		t.Fatalf("reloaded record = %+v, ok=%v", record, ok)
	}
	if reloaded.Count() != 1 {
		t.Fatalf("record count = %d, want 1", reloaded.Count())
	}
}

func TestRankingHandlerPersistsUploadBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranking.jsonl")
	store, err := NewJSONLCommonDataStore(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := testSettings()
	endpoint := NewEndpoint(settings)
	conn := NewConnection(endpoint, "127.0.0.1:1", func([]byte) {})
	conn.PID = 99
	body := []byte{9, 8, 7}
	resp := RankingHandlerWithStore(store)(conn, NewRMCRequest(settings, ProtocolRanking, MethodUploadCommonData, 1, body))
	if resp == nil || resp.IsError {
		t.Fatalf("unexpected response: %+v", resp)
	}
	record, ok := store.Latest(99)
	if !ok || !bytes.Equal(record.Data, body) {
		t.Fatalf("stored record = %+v, ok=%v", record, ok)
	}
}
