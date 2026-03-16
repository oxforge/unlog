package filter

import (
	"sync"
	"time"

	"github.com/oxforge/unlog/types"
)

// dedupEntry tracks a deduplicated log message signature.
type dedupEntry struct {
	first    types.LogEntry
	lastSeen time.Time
	count    int
	sig      string
}

// dedupShard is a concurrency-safe partition of the dedup cache.
type dedupShard struct {
	mu    sync.Mutex
	cache *lru[string, *dedupEntry]
}

// DedupFilter performs fuzzy deduplication of log entries using signature extraction
// and a sharded LRU cache for concurrent access.
type DedupFilter struct {
	shards  []*dedupShard
	maxDups int
}

// NewDedupFilter creates a DedupFilter with the given maximum duplicate count,
// number of shards for concurrency, and total cache size across all shards.
func NewDedupFilter(maxDups, numShards, cacheSize int) *DedupFilter {
	if numShards < 1 {
		numShards = 1
	}
	perShard := cacheSize / numShards
	if perShard < 1 {
		perShard = 1
	}
	shards := make([]*dedupShard, numShards)
	for i := range shards {
		shards[i] = &dedupShard{
			cache: newLRU[string, *dedupEntry](perShard),
		}
	}
	return &DedupFilter{
		shards:  shards,
		maxDups: maxDups,
	}
}

// shardFor selects a shard using inline FNV-1a hash to avoid allocations.
func (d *DedupFilter) shardFor(sig string) *dedupShard {
	var h uint32 = 2166136261
	for i := 0; i < len(sig); i++ {
		h ^= uint32(sig[i])
		h *= 16777619
	}
	return d.shards[h%uint32(len(d.shards))]
}

// Apply checks whether the entry should be kept based on its fuzzy signature.
func (d *DedupFilter) Apply(entry types.LogEntry) (types.FilteredEntry, bool) {
	sig := ExtractSignature(entry.Message)
	shard := d.shardFor(sig)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	existing, ok := shard.cache.Get(sig)
	if !ok {
		shard.cache.Put(sig, &dedupEntry{
			first:    entry,
			lastSeen: entry.Timestamp,
			count:    1,
			sig:      sig,
		})
		return types.FilteredEntry{
			LogEntry:        entry,
			OccurrenceCount: 1,
			FirstSeen:       entry.Timestamp,
			LastSeen:        entry.Timestamp,
			Signature:       sig,
		}, true
	}

	existing.count++
	if entry.Timestamp.After(existing.lastSeen) {
		existing.lastSeen = entry.Timestamp
	}

	if existing.count <= d.maxDups {
		return types.FilteredEntry{
			LogEntry:        entry,
			OccurrenceCount: existing.count,
			FirstSeen:       existing.first.Timestamp,
			LastSeen:        entry.Timestamp,
			Signature:       sig,
		}, true
	}

	return types.FilteredEntry{}, false
}

// Summaries returns synthetic FilteredEntry values for every signature whose
// count exceeded maxDups.
func (d *DedupFilter) Summaries() []types.FilteredEntry {
	var out []types.FilteredEntry
	for _, shard := range d.shards {
		shard.mu.Lock()
		shard.cache.Each(func(_ string, de *dedupEntry) {
			if de.count > d.maxDups {
				out = append(out, types.FilteredEntry{
					LogEntry: types.LogEntry{
						Timestamp: de.first.Timestamp,
						Level:     de.first.Level,
						Source:    de.first.Source,
						Message:   de.first.Message,
					},
					OccurrenceCount: de.count,
					FirstSeen:       de.first.Timestamp,
					LastSeen:        de.lastSeen,
					IsSpike:         false,
					Signature:       de.sig,
				})
			}
		})
		shard.mu.Unlock()
	}
	return out
}

// UniqueSignatures returns the total number of unique signatures across all shards.
func (d *DedupFilter) UniqueSignatures() int {
	total := 0
	for _, shard := range d.shards {
		shard.mu.Lock()
		total += shard.cache.Len()
		shard.mu.Unlock()
	}
	return total
}
