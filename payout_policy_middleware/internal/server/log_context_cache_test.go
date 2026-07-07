package server

import (
	"testing"
	"time"

	"payout_policy_middleware/internal/robosats"
)

func TestLNPaymentLogContextCacheCachesAndExpires(t *testing.T) {
	cache := newLNPaymentLogContextCache(20*time.Millisecond, 4)
	row := &robosats.LNPaymentLogContext{PaymentHash: "abc", NumSatoshis: 123}
	cache.set("hash:abc", row)

	row.NumSatoshis = 999
	cached, ok := cache.get("hash:abc")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if cached.NumSatoshis != 123 {
		t.Fatalf("cache stored mutable row: %#v", cached)
	}

	cached.NumSatoshis = 777
	cachedAgain, ok := cache.get("hash:abc")
	if !ok {
		t.Fatal("expected second cache hit")
	}
	if cachedAgain.NumSatoshis != 123 {
		t.Fatalf("cache returned mutable row: %#v", cachedAgain)
	}

	time.Sleep(25 * time.Millisecond)
	if _, ok := cache.get("hash:abc"); ok {
		t.Fatal("expected cache entry to expire")
	}
}

func TestReadinessCacheCachesAndExpires(t *testing.T) {
	cache := readinessCache{}
	payload := readyzPayload{
		OK:     true,
		Checks: map[string]bool{"postgres": true},
	}
	cache.set(20*time.Millisecond, 200, payload)

	payload.Checks["postgres"] = false
	status, cached, ok := cache.get()
	if !ok {
		t.Fatal("expected readiness cache hit")
	}
	if status != 200 || !cached.Checks["postgres"] {
		t.Fatalf("unexpected cached readiness: status=%d payload=%#v", status, cached)
	}

	cached.Checks["postgres"] = false
	_, cachedAgain, ok := cache.get()
	if !ok {
		t.Fatal("expected second readiness cache hit")
	}
	if !cachedAgain.Checks["postgres"] {
		t.Fatalf("readiness cache returned mutable payload: %#v", cachedAgain)
	}

	time.Sleep(25 * time.Millisecond)
	if _, _, ok := cache.get(); ok {
		t.Fatal("expected readiness cache entry to expire")
	}
}
