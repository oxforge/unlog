package filter

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oxforge/unlog/types"
)

// dedupKey identifies a record: one per error per source and service.
type dedupKey struct {
	source  string
	service string
	sig     string
}

// dedupRecord accumulates every occurrence of one error in one source and service.
type dedupRecord struct {
	exemplar types.LogEntry
	sig      string
	count    int64
	first    time.Time
	last     time.Time
	level    types.Level
	byLevel  [types.LevelFatal + 1]int // occurrences per level
	spike    bool
}

// dedupShard is a concurrency-safe partition of the record map.
type dedupShard struct {
	mu      sync.Mutex
	records map[dedupKey]*dedupRecord
	ids     map[dedupKey]uint32
}

// DedupFilter merges log entries into one record per (source, service, fuzzy
// signature).
// The result does not depend on the order entries arrive in.
type DedupFilter struct {
	shards []*dedupShard
	nextID atomic.Uint32
	byID   sync.Map // uint32 -> *dedupRecord
	// exact, when set, picks entries merged only with identical messages.
	exact func(types.LogEntry) bool
}

// NewDedupFilter creates a DedupFilter with the given number of shards.
func NewDedupFilter(numShards int) *DedupFilter {
	if numShards < 1 {
		numShards = 1
	}
	shards := make([]*dedupShard, numShards)
	for i := range shards {
		shards[i] = &dedupShard{
			records: make(map[dedupKey]*dedupRecord),
			ids:     make(map[dedupKey]uint32),
		}
	}
	return &DedupFilter{shards: shards}
}

// shardFor selects a shard using inline FNV-1a hash to avoid allocations.
func (d *DedupFilter) shardFor(k dedupKey) *dedupShard {
	var h uint32 = 2166136261
	for i := 0; i < len(k.source); i++ {
		h ^= uint32(k.source[i])
		h *= 16777619
	}
	h ^= 0xff
	h *= 16777619
	for i := 0; i < len(k.sig); i++ {
		h ^= uint32(k.sig[i])
		h *= 16777619
	}
	return d.shards[h%uint32(len(d.shards))]
}

// betterExemplar reports whether e should replace the record's current exemplar:
// the earliest dated occurrence wins; ties go to the smaller source, then line.
// An undated occurrence only counts while the record has no dated one.
func betterExemplar(e, cur types.LogEntry) bool {
	switch {
	case e.Timestamp.IsZero() != cur.Timestamp.IsZero():
		return !e.Timestamp.IsZero()
	case !e.Timestamp.Equal(cur.Timestamp):
		return e.Timestamp.Before(cur.Timestamp)
	case e.Source != cur.Source:
		return e.Source < cur.Source
	default:
		return e.LineNumber < cur.LineNumber
	}
}

// Add merges the entry into the record keyed by (Source, Service, signature)
// and returns the record's id; an entry exact accepts is keyed by its whole
// message instead. It is safe for concurrent use.
func (d *DedupFilter) Add(entry types.LogEntry) uint32 {
	sig := entry.Message
	if d.exact == nil || !d.exact(entry) {
		sig = ExtractSignature(entry.Message)
	}
	k := dedupKey{source: entry.Source, service: entry.Service, sig: sig}
	shard := d.shardFor(k)

	shard.mu.Lock()
	defer shard.mu.Unlock()

	rec, ok := shard.records[k]
	if !ok {
		rec = &dedupRecord{exemplar: entry, sig: k.sig, level: entry.Level}
		id := d.nextID.Add(1)
		shard.records[k] = rec
		shard.ids[k] = id
		d.byID.Store(id, rec)
	} else if betterExemplar(entry, rec.exemplar) {
		rec.exemplar = entry
	}
	rec.count++
	if entry.Level >= 0 && entry.Level <= types.LevelFatal {
		rec.byLevel[entry.Level]++
	}
	if entry.Level > rec.level {
		rec.level = entry.Level
	}
	if ts := entry.Timestamp; !ts.IsZero() {
		if rec.first.IsZero() || ts.Before(rec.first) {
			rec.first = ts
		}
		if ts.After(rec.last) {
			rec.last = ts
		}
	}
	return shard.ids[k]
}

// record returns the record with the given id, or nil.
func (d *DedupFilter) record(id uint32) *dedupRecord {
	v, ok := d.byID.Load(id)
	if !ok {
		return nil
	}
	return v.(*dedupRecord)
}

// markSpike flags the record as part of a burst. Call it only after all Add calls.
func (d *DedupFilter) markSpike(id uint32) {
	if r := d.record(id); r != nil {
		r.spike = true
	}
}

// Entries returns one FilteredEntry per record, sorted by FirstSeen (undated
// last), then source, then exemplar line number. Call it only after all Add calls.
func (d *DedupFilter) Entries() []types.FilteredEntry {
	var out []types.FilteredEntry
	for _, shard := range d.shards {
		shard.mu.Lock()
		for _, r := range shard.records {
			le := r.exemplar
			le.Level = r.level
			le.Timestamp = r.first
			byLevel := make(map[types.Level]int)
			for lvl, n := range r.byLevel {
				if n > 0 {
					byLevel[types.Level(lvl)] = n
				}
			}
			out = append(out, types.FilteredEntry{
				LogEntry:           le,
				OccurrenceCount:    int(r.count),
				OccurrencesByLevel: byLevel,
				FirstSeen:          r.first,
				LastSeen:           r.last,
				IsSpike:            r.spike,
				Signature:          r.sig,
			})
		}
		shard.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.FirstSeen.IsZero() != b.FirstSeen.IsZero() {
			return !a.FirstSeen.IsZero()
		}
		if !a.FirstSeen.Equal(b.FirstSeen) {
			return a.FirstSeen.Before(b.FirstSeen)
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		if a.LineNumber != b.LineNumber {
			return a.LineNumber < b.LineNumber
		}
		return a.Signature < b.Signature
	})
	return out
}

// UniqueSignatures returns the number of distinct signatures, ignoring source.
func (d *DedupFilter) UniqueSignatures() int {
	sigs := make(map[string]struct{})
	for _, shard := range d.shards {
		shard.mu.Lock()
		for k := range shard.records {
			sigs[k.sig] = struct{}{}
		}
		shard.mu.Unlock()
	}
	return len(sigs)
}
