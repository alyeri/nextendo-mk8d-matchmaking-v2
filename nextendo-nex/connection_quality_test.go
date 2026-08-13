package nex

import (
	"testing"
	"time"
)

func TestConnectionQualityRecordsOnlyObservedData(t *testing.T) {
	now := time.Unix(1000, 0)
	r := NewConnectionQualityRegistry()
	r.now = func() time.Time { return now }

	station := NewStationURL("prudp")
	station.Set("address", "203.0.113.9")
	station.SetInt("port", 63052)
	station.SetInt("RVCID", 42)
	station.SetInt("natm", 2)
	station.SetInt("natf", 1)
	r.Observe(77, []*StationURL{station}, true)

	q, ok := r.Lookup(77)
	if !ok || !q.DirectReady || !q.ReplaceSeen {
		t.Fatalf("unexpected quality snapshot: ok=%v q=%+v", ok, q)
	}
	if !q.HasNATMapping || q.NATMapping != 2 || !q.HasNATFiltering || q.NATFiltering != 1 {
		t.Fatalf("NAT values were not preserved: %+v", q)
	}
	if score := r.PairScore(88, 77); score != 100 {
		t.Fatalf("fresh complete host score = %d, want 100", score)
	}

	now = now.Add(3 * time.Minute)
	if score := r.PairScore(88, 77); score != 0 {
		t.Fatalf("stale host score = %d, want 0", score)
	}
}

func TestConnectionQualityDoesNotInventNATValues(t *testing.T) {
	r := NewConnectionQualityRegistry()
	station := ParseStationURL("prudp:/address=192.168.1.8;port=5000;RVCID=3")
	r.Observe(9, []*StationURL{station}, false)
	q, ok := r.Lookup(9)
	if !ok {
		t.Fatal("quality observation missing")
	}
	if q.HasNATMapping || q.HasNATFiltering {
		t.Fatalf("missing NAT fields must remain unknown: %+v", q)
	}
}
