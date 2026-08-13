package main

import (
	"testing"
	"time"
)

func TestRequestLimiterBlocksAndRecovers(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := newRequestLimiter()
	limiter.now = func() time.Time { return now }
	policy := limitPolicy{name: "test", limit: 2, window: time.Minute, baseBlock: 10 * time.Second, maxBlock: time.Minute}

	if !limiter.allow("ip:1", policy).allowed || !limiter.allow("ip:1", policy).allowed {
		t.Fatal("requests inside limit were rejected")
	}
	blocked := limiter.allow("ip:1", policy)
	if blocked.allowed || !blocked.newBlock || blocked.retryIn != 10*time.Second {
		t.Fatalf("unexpected first block: %+v", blocked)
	}
	if limiter.allow("ip:1", policy).allowed {
		t.Fatal("request inside block was accepted")
	}
	now = now.Add(11 * time.Second)
	if !limiter.allow("ip:1", policy).allowed {
		t.Fatal("request after temporary block was rejected")
	}
}

func TestRequestLimiterSeparatesSubjects(t *testing.T) {
	limiter := newRequestLimiter()
	policy := limitPolicy{name: "test", limit: 1, window: time.Minute, baseBlock: time.Second, maxBlock: time.Minute}
	if !limiter.allow("pid:1", policy).allowed || !limiter.allow("pid:2", policy).allowed {
		t.Fatal("independent PIDs shared a rate bucket")
	}
	if limiter.allow("pid:1", policy).allowed {
		t.Fatal("repeated PID was not limited")
	}
}
