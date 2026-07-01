package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// bucket is a thin wrapper over x/time/rate.Limiter (a token bucket: Burst is
// capacity, RPS is the refill rate).
type bucket struct {
	lim *rate.Limiter
}

func newBucket(rps float64, burst int) *bucket {
	if burst < 1 {
		burst = 1
	}
	return &bucket{lim: rate.NewLimiter(rate.Limit(rps), burst)}
}

func (b *bucket) allow() bool { return b.lim.Allow() }

// keyedBuckets maintains one token bucket per key (IP or user id). Idle buckets
// are evicted by a background sweeper so memory does not grow unbounded with
// the number of distinct clients — important for a public edge.
type keyedBuckets struct {
	mu      sync.Mutex
	buckets map[string]*keyedEntry
	rps     rate.Limit
	burst   int
	ttl     time.Duration
	done    chan struct{}
	stopOne sync.Once
}

type keyedEntry struct {
	lim      *rate.Limiter
	lastSeen time.Time
}

func newKeyedBuckets(rps float64, burst int) *keyedBuckets {
	if burst < 1 {
		burst = 1
	}
	k := &keyedBuckets{
		buckets: make(map[string]*keyedEntry),
		rps:     rate.Limit(rps),
		burst:   burst,
		ttl:     10 * time.Minute,
		done:    make(chan struct{}),
	}
	go k.sweep()
	return k
}

func (k *keyedBuckets) allow(key string) bool {
	k.mu.Lock()
	e, ok := k.buckets[key]
	if !ok {
		e = &keyedEntry{lim: rate.NewLimiter(k.rps, k.burst)}
		k.buckets[key] = e
	}
	e.lastSeen = time.Now()
	lim := e.lim
	k.mu.Unlock()
	return lim.Allow()
}

// sweep periodically evicts buckets unused for longer than ttl.
func (k *keyedBuckets) sweep() {
	ticker := time.NewTicker(k.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-k.done:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-k.ttl)
			k.mu.Lock()
			for key, e := range k.buckets {
				if e.lastSeen.Before(cutoff) {
					delete(k.buckets, key)
				}
			}
			k.mu.Unlock()
		}
	}
}

func (k *keyedBuckets) stop() { k.stopOne.Do(func() { close(k.done) }) }
