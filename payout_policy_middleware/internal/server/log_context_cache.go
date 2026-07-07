package server

import (
	"sync"
	"time"

	"payout_policy_middleware/internal/robosats"
)

type lnPaymentLogContextCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	items map[string]lnPaymentLogContextEntry
}

type lnPaymentLogContextEntry struct {
	row       *robosats.LNPaymentLogContext
	expiresAt time.Time
}

func newLNPaymentLogContextCache(ttl time.Duration, max int) *lnPaymentLogContextCache {
	if ttl <= 0 || max <= 0 {
		return nil
	}
	return &lnPaymentLogContextCache{
		ttl:   ttl,
		max:   max,
		items: map[string]lnPaymentLogContextEntry{},
	}
}

func (c *lnPaymentLogContextCache) get(key string) (*robosats.LNPaymentLogContext, bool) {
	if c == nil || key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.items, key)
		return nil, false
	}
	return cloneLNPaymentLogContext(entry.row), true
}

func (c *lnPaymentLogContextCache) set(key string, row *robosats.LNPaymentLogContext) {
	if c == nil || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.max {
		c.pruneExpiredLocked(time.Now())
	}
	if len(c.items) >= c.max {
		for oldKey := range c.items {
			delete(c.items, oldKey)
			break
		}
	}
	c.items[key] = lnPaymentLogContextEntry{
		row:       cloneLNPaymentLogContext(row),
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *lnPaymentLogContextCache) pruneExpiredLocked(now time.Time) {
	for key, entry := range c.items {
		if now.After(entry.expiresAt) {
			delete(c.items, key)
		}
	}
}

func cloneLNPaymentLogContext(row *robosats.LNPaymentLogContext) *robosats.LNPaymentLogContext {
	if row == nil {
		return nil
	}
	copy := *row
	return &copy
}
