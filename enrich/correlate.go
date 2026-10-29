package enrich

import (
	"sort"
	"time"

	"github.com/oxforge/unlog/types"
)

// maxCorrelatedNames caps how many origins CorrelatedWith lists, so a storm
// across hundreds of services costs a few strings per entry.
const maxCorrelatedNames = 3

// Correlator tags errors that started within a window of errors from other
// origins: failures that happened together.
type Correlator struct {
	window time.Duration
}

// NewCorrelator creates a Correlator with the given correlation window.
func NewCorrelator(window time.Duration) *Correlator {
	return &Correlator{window: window}
}

// Correlate sets CorrelatedWith and CorrelatedCount on every dated ERROR or
// FATAL entry: the other origins (source, plus service when the line names
// one) with an error that started within the window, before or after it.
// Undated entries and those below ERROR are neither tagged nor counted. A
// line that names no service isn't counted against the services in its own
// file, since it may come from any of them.
//
// It sweeps the errors in time order, keeping the origins inside the window
// as a sorted set, so the cost grows with the entries, not their square.
func (c *Correlator) Correlate(entries []types.EnrichedEntry) {
	var order []int
	for i := range entries {
		if entries[i].Level >= types.LevelError && !entries[i].Timestamp.IsZero() {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return entries[order[a]].Timestamp.Before(entries[order[b]].Timestamp)
	})

	w := newWindowSet()
	lo, hi := 0, 0
	for _, i := range order {
		at := entries[i].Timestamp
		for hi < len(order) && !entries[order[hi]].Timestamp.After(at.Add(c.window)) {
			w.add(&entries[order[hi]])
			hi++
		}
		for entries[order[lo]].Timestamp.Before(at.Add(-c.window)) {
			w.remove(&entries[order[lo]])
			lo++
		}
		w.tag(&entries[i])
	}
}

// windowSet counts the errors of each origin inside the window.
type windowSet struct {
	count   map[string]int // origin → errors in the window
	files   map[string]int // source → its origins in the window
	origins []windowOrigin // the origins in the window, sorted by name
}

type windowOrigin struct{ name, source, service string }

func newWindowSet() *windowSet {
	return &windowSet{count: map[string]int{}, files: map[string]int{}}
}

func (w *windowSet) add(e *types.EnrichedEntry) {
	name := e.Origin()
	w.count[name]++
	if w.count[name] > 1 {
		return
	}
	w.files[e.Source]++
	at := sort.Search(len(w.origins), func(k int) bool { return w.origins[k].name >= name })
	w.origins = append(w.origins, windowOrigin{})
	copy(w.origins[at+1:], w.origins[at:])
	w.origins[at] = windowOrigin{name: name, source: e.Source, service: e.Service}
}

func (w *windowSet) remove(e *types.EnrichedEntry) {
	name := e.Origin()
	w.count[name]--
	if w.count[name] > 0 {
		return
	}
	delete(w.count, name)
	w.files[e.Source]--
	at := sort.Search(len(w.origins), func(k int) bool { return w.origins[k].name >= name })
	w.origins = append(w.origins[:at], w.origins[at+1:]...)
}

// tag sets e's correlation from the origins in the window.
func (w *windowSet) tag(e *types.EnrichedEntry) {
	// Origins not counted for e: its own, and in its own file, every other
	// one when e names no service, or else the one that names none.
	skipped := 1
	if e.Service == "" {
		skipped = w.files[e.Source]
	} else if w.count[e.Source] > 0 {
		skipped++
	}
	total := len(w.origins) - skipped
	if total <= 0 {
		return
	}
	names := make([]string, 0, min(total, maxCorrelatedNames))
	for _, o := range w.origins {
		if len(names) == maxCorrelatedNames {
			break
		}
		if o.source == e.Source && (o.service == e.Service || o.service == "" || e.Service == "") {
			continue
		}
		names = append(names, o.name)
	}
	e.CorrelatedWith, e.CorrelatedCount = names, total
}
