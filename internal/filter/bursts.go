package filter

import (
	"sort"
	"time"

	"github.com/oxforge/unlog/internal/types"
)

// maxTopErrors is the number of errors reported per burst.
const maxTopErrors = 3

// occurrence notes one dated entry: the record it merged into and its unix second.
type occurrence struct {
	record uint32
	second int64
}

// secondCount is the number of events a source logged in one second.
type secondCount struct {
	second int64
	count  int64
}

// burstOrigin is where a burst happened: a source, and the service within it
// when the lines name one.
type burstOrigin struct {
	source  string
	service string
}

// detectBursts finds, per source and service, runs of seconds far above its median
// rate, marks every record with an occurrence inside one, and returns the bursts
// sorted by Start, then Source. The result does not depend on the order of notes.
// Call it only after all DedupFilter.Add calls.
func detectBursts(notes []occurrence, d *DedupFilter, multiplier float64, minEvents int64) []types.Burst {
	byOrigin := make(map[burstOrigin][]occurrence)
	for _, n := range notes {
		r := d.record(n.record)
		if r == nil {
			continue
		}
		o := burstOrigin{source: r.exemplar.Source, service: r.exemplar.Service}
		byOrigin[o] = append(byOrigin[o], n)
	}

	var bursts []types.Burst
	for o, originNotes := range byOrigin {
		bursts = append(bursts, sourceBursts(o, originNotes, d, multiplier, minEvents)...)
	}
	sort.Slice(bursts, func(i, j int) bool {
		if !bursts[i].Start.Equal(bursts[j].Start) {
			return bursts[i].Start.Before(bursts[j].Start)
		}
		if bursts[i].Source != bursts[j].Source {
			return bursts[i].Source < bursts[j].Source
		}
		return bursts[i].Service < bursts[j].Service
	})
	return bursts
}

// sourceBursts detects the bursts of one source and service and marks their
// records.
func sourceBursts(src burstOrigin, notes []occurrence, d *DedupFilter, multiplier float64, minEvents int64) []types.Burst {
	sort.Slice(notes, func(i, j int) bool {
		if notes[i].second != notes[j].second {
			return notes[i].second < notes[j].second
		}
		return notes[i].record < notes[j].record
	})

	var counts []secondCount
	for _, n := range notes {
		if last := len(counts) - 1; last >= 0 && counts[last].second == n.second {
			counts[last].count++
		} else {
			counts = append(counts, secondCount{second: n.second, count: 1})
		}
	}

	span := counts[len(counts)-1].second - counts[0].second + 1
	baseline := medianCount(counts, span)
	// A second is hot above multiplier x baseline, and always needs at least
	// two events: with a baseline of 0, a lone stray error must not start or
	// extend a burst.
	threshold := max(multiplier*baseline, 1)

	var bursts []types.Burst
	runStart, runEnd := int64(0), int64(0)
	inRun := false
	flush := func() {
		if !inRun {
			return
		}
		if b, ok := buildBurst(src, notes, runStart, runEnd, baseline, d, minEvents); ok {
			bursts = append(bursts, b)
		}
		inRun = false
	}
	for _, c := range counts {
		if float64(c.count) <= threshold {
			continue
		}
		if inRun && c.second-runEnd <= 2 {
			runEnd = c.second
			continue
		}
		flush()
		runStart, runEnd, inRun = c.second, c.second, true
	}
	flush()
	return bursts
}

// medianCount returns the median per-second count over span seconds, where the
// seconds missing from counts hold 0.
func medianCount(counts []secondCount, span int64) float64 {
	zeros := span - int64(len(counts))
	vals := make([]int64, len(counts))
	for i, c := range counts {
		vals[i] = c.count
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	at := func(i int64) int64 { // i-th smallest of the full span
		if i < zeros {
			return 0
		}
		return vals[i-zeros]
	}
	if span%2 == 1 {
		return float64(at(span / 2))
	}
	return float64(at(span/2-1)+at(span/2)) / 2
}

// buildBurst turns the run [start, end] into a Burst when it holds enough
// events, marking the records that occur inside it. notes must be sorted by second.
func buildBurst(
	src burstOrigin, notes []occurrence, start, end int64, baseline float64, d *DedupFilter, minEvents int64,
) (types.Burst, bool) {
	lo := sort.Search(len(notes), func(i int) bool { return notes[i].second >= start })
	hi := sort.Search(len(notes), func(i int) bool { return notes[i].second > end })
	events := int64(hi - lo)
	if events < minEvents {
		return types.Burst{}, false
	}

	perRecord := make(map[uint32]int64)
	for _, n := range notes[lo:hi] {
		perRecord[n.record]++
	}
	ids := make([]uint32, 0, len(perRecord))
	for id := range perRecord {
		d.markSpike(id)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := ids[i], ids[j]
		if perRecord[a] != perRecord[b] {
			return perRecord[a] > perRecord[b]
		}
		ra, rb := d.record(a), d.record(b)
		if !ra.first.Equal(rb.first) {
			return ra.first.Before(rb.first)
		}
		return ra.sig < rb.sig
	})
	if len(ids) > maxTopErrors {
		ids = ids[:maxTopErrors]
	}
	top := make([]types.BurstError, 0, len(ids))
	for _, id := range ids {
		r := d.record(id)
		top = append(top, types.BurstError{Message: r.exemplar.Message, Level: r.level, Count: perRecord[id]})
	}

	return types.Burst{
		Source:    src.source,
		Service:   src.service,
		Start:     time.Unix(start, 0).UTC(),
		End:       time.Unix(end, 0).UTC(),
		Events:    events,
		Baseline:  baseline,
		TopErrors: top,
	}, true
}
