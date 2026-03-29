package rain

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildQueryCacheKeyIgnoresTags(t *testing.T) {
	t.Parallel()

	a := normalizeQueryCacheOptions(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users", "active"}, Namespace: "list"})
	b := normalizeQueryCacheOptions(QueryCacheOptions{TTL: time.Minute, Tags: []string{"users", "stale"}, Namespace: "list"})

	keyA, err := buildQueryCacheKey("sqlite", "SELECT * FROM users WHERE active = ?", []any{true}, nil, a)
	if err != nil {
		t.Fatalf("build key A: %v", err)
	}
	keyB, err := buildQueryCacheKey("sqlite", "SELECT * FROM users WHERE active = ?", []any{true}, nil, b)
	if err != nil {
		t.Fatalf("build key B: %v", err)
	}
	if keyA != keyB {
		t.Fatalf("expected identical keys when only tags differ, got %q and %q", keyA, keyB)
	}
}

func TestMemoryQueryCacheGetDoesNotDeleteFreshOverwrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cache := NewMemoryQueryCache()
	base := time.Date(2026, 3, 29, 13, 0, 0, 0, time.UTC)
	cache.now = func() time.Time { return base }

	if err := cache.Set(ctx, "k", []byte("old"), time.Second, []string{"users"}); err != nil {
		t.Fatalf("set old entry: %v", err)
	}

	allowFirstNow := make(chan struct{})
	reachedFirstNow := make(chan struct{})
	var nowCalls int32
	cache.now = func() time.Time {
		call := atomic.AddInt32(&nowCalls, 1)
		if call == 1 {
			close(reachedFirstNow)
			<-allowFirstNow
		}
		return base.Add(2 * time.Second)
	}

	resultCh := make(chan bool, 1)
	go func() {
		_, ok, _ := cache.Get(ctx, "k")
		resultCh <- ok
	}()
	<-reachedFirstNow

	if err := cache.Set(ctx, "k", []byte("new"), time.Second, []string{"users"}); err != nil {
		t.Fatalf("set new entry: %v", err)
	}
	close(allowFirstNow)

	if ok := <-resultCh; ok {
		t.Fatalf("expected stale read to miss once old entry is expired")
	}

	value, ok, err := cache.Get(ctx, "k")
	if err != nil {
		t.Fatalf("get fresh entry: %v", err)
	}
	if !ok {
		t.Fatalf("expected fresh overwrite to remain cached")
	}
	if string(value) != "new" {
		t.Fatalf("expected fresh value \"new\", got %q", string(value))
	}
}
