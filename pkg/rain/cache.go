package rain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// QueryCache is the pluggable backend interface for opt-in SELECT query caching.
type QueryCache interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration, tags []string) error
	InvalidateTags(ctx context.Context, tags ...string) error
}

// QueryCacheOptions configures per-query cache behavior for SELECT helpers.
type QueryCacheOptions struct {
	TTL       time.Duration
	Tags      []string
	Namespace string
	Key       string
	Bypass    bool
}

type queryCacheOptions struct {
	ttl       time.Duration
	tags      []string
	namespace string
	key       string
	bypass    bool
}

type queryCacheKeyMaterial struct {
	Dialect   string   `json:"dialect"`
	SQL       string   `json:"sql"`
	Args      []any    `json:"args"`
	Namespace string   `json:"namespace,omitempty"`
	Relations []string `json:"relations,omitempty"`
}

func normalizeQueryCacheOptions(opts QueryCacheOptions) *queryCacheOptions {
	if opts.TTL <= 0 {
		return nil
	}

	normalizedTags := append([]string(nil), opts.Tags...)
	sort.Strings(normalizedTags)

	return &queryCacheOptions{
		ttl:       opts.TTL,
		tags:      normalizedTags,
		namespace: strings.TrimSpace(opts.Namespace),
		key:       strings.TrimSpace(opts.Key),
		bypass:    opts.Bypass,
	}
}

func buildQueryCacheKey(dialectName string, sqlText string, args []any, relationNames []string, opts *queryCacheOptions) (string, error) {
	if opts != nil && opts.key != "" {
		return opts.key, nil
	}

	relations := append([]string(nil), relationNames...)
	sort.Strings(relations)

	material := queryCacheKeyMaterial{
		Dialect:   dialectName,
		SQL:       sqlText,
		Args:      args,
		Relations: relations,
	}
	if opts != nil {
		material.Namespace = opts.namespace
	}

	payload, err := json.Marshal(material)
	if err != nil {
		return "", fmt.Errorf("rain: encode query cache key: %w", err)
	}
	hash := sha256.Sum256(payload)
	return "rain:query:" + hex.EncodeToString(hash[:]), nil
}

// MemoryQueryCache is an in-memory QueryCache backend for local use and tests.
type MemoryQueryCache struct {
	mu         sync.RWMutex
	entries    map[string]memoryQueryCacheEntry
	tagMembers map[string]map[string]struct{}
	now        func() time.Time
	seq        uint64
}

type memoryQueryCacheEntry struct {
	version   uint64
	value     []byte
	expiresAt time.Time
	tags      []string
}

// NewMemoryQueryCache creates a new in-memory query cache.
func NewMemoryQueryCache() *MemoryQueryCache {
	return &MemoryQueryCache{
		entries:    make(map[string]memoryQueryCacheEntry),
		tagMembers: make(map[string]map[string]struct{}),
		now:        time.Now,
	}
}

func (c *MemoryQueryCache) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false, nil
	}
	if c.now().After(entry.expiresAt) {
		c.mu.Lock()
		current, stillPresent := c.entries[key]
		if stillPresent && current.version == entry.version && c.now().After(current.expiresAt) {
			c.deleteEntryLocked(key, current)
		}
		c.mu.Unlock()
		return nil, false, nil
	}
	copied := append([]byte(nil), entry.value...)
	return copied, true, nil
}

func (c *MemoryQueryCache) Set(_ context.Context, key string, value []byte, ttl time.Duration, tags []string) error {
	if ttl <= 0 {
		return nil
	}
	entry := memoryQueryCacheEntry{
		value:     append([]byte(nil), value...),
		expiresAt: c.now().Add(ttl),
		tags:      append([]string(nil), tags...),
	}

	c.mu.Lock()
	c.seq++
	entry.version = c.seq
	if prev, ok := c.entries[key]; ok {
		c.deleteEntryLocked(key, prev)
	}
	c.entries[key] = entry
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			continue
		}
		members, ok := c.tagMembers[tag]
		if !ok {
			members = make(map[string]struct{})
			c.tagMembers[tag] = members
		}
		members[key] = struct{}{}
	}
	c.mu.Unlock()

	return nil
}

func (c *MemoryQueryCache) InvalidateTags(_ context.Context, tags ...string) error {
	c.mu.Lock()
	for _, tag := range tags {
		members := c.tagMembers[tag]
		for key := range members {
			if entry, ok := c.entries[key]; ok {
				c.deleteEntryLocked(key, entry)
			}
		}
		delete(c.tagMembers, tag)
	}
	c.mu.Unlock()

	return nil
}

func (c *MemoryQueryCache) deleteEntryLocked(key string, entry memoryQueryCacheEntry) {
	delete(c.entries, key)
	for _, tag := range entry.tags {
		members, ok := c.tagMembers[tag]
		if !ok {
			continue
		}
		delete(members, key)
		if len(members) == 0 {
			delete(c.tagMembers, tag)
		}
	}
}
