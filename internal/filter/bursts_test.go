package filter

import (
	"fmt"
	"testing"
	"time"

	"github.com/oxforge/unlog/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var burstBase = time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

// noteSet feeds entries through a real DedupFilter and collects the notes.
type noteSet struct {
	d     *DedupFilter
	notes []occurrence
}

func newNoteSet() *noteSet { return &noteSet{d: NewDedupFilter(4)} }

func (n *noteSet) add(ts time.Time, source, msg string) {
	id := n.d.Add(types.LogEntry{Timestamp: ts, Level: types.LevelError, Source: source, Message: msg})
	if !ts.IsZero() {
		n.notes = append(n.notes, occurrence{record: id, second: ts.Unix()})
	}
}

func (n *noteSet) detect() []types.Burst { return detectBursts(n.notes, n.d, 10, 20) }

func (n *noteSet) spikeOf(sigMsg string) bool {
	for _, e := range n.d.Entries() {
		if e.Signature == ExtractSignature(sigMsg) {
			return e.IsSpike
		}
	}
	panic("no such record: " + sigMsg)
}

func (n *noteSet) addBackground() {
	for i := 0; i < 60; i++ {
		n.add(burstBase.Add(time.Duration(i)*10*time.Second), "same.log", fmt.Sprintf("cache miss for key user:%d", i))
	}
}

func (n *noteSet) addPaymentBurst(start time.Time) {
	for i := 0; i < 400; i++ {
		n.add(start.Add(time.Duration(i)*25*time.Millisecond), "same.log",
			fmt.Sprintf("payment gateway refused connection from 10.0.3.%d", i))
	}
}

func TestBurstStandsOutFromBackground(t *testing.T) {
	n := newNoteSet()
	n.addBackground()
	n.addPaymentBurst(burstBase.Add(5 * time.Minute))

	bursts := n.detect()

	require.Len(t, bursts, 1)
	b := bursts[0]
	assert.Equal(t, "same.log", b.Source)
	assert.Equal(t, burstBase.Add(5*time.Minute), b.Start)
	assert.Equal(t, burstBase.Add(5*time.Minute+9*time.Second), b.End)
	assert.Equal(t, int64(401), b.Events)
	assert.Equal(t, 0.0, b.Baseline)
	require.Len(t, b.TopErrors, 2)
	assert.Equal(t, "payment gateway refused connection from 10.0.3.0", b.TopErrors[0].Message)
	assert.Equal(t, int64(400), b.TopErrors[0].Count)
	assert.Equal(t, "cache miss for key user:0", b.TopErrors[1].Message)
	assert.Equal(t, int64(1), b.TopErrors[1].Count)
	assert.True(t, n.spikeOf("payment gateway refused connection from 10.0.3.0"))
	assert.True(t, n.spikeOf("cache miss for key user:0"))
}

func TestNoBurstInSteadyStream(t *testing.T) {
	n := newNoteSet()
	for s := 0; s < 3600; s++ {
		for k := 0; k < 5; k++ {
			n.add(burstBase.Add(time.Duration(s)*time.Second), "a.log", "steady failure")
		}
	}
	assert.Empty(t, n.detect())
}

func TestNoBurstFromOneBusySecond(t *testing.T) {
	n := newNoteSet()
	n.add(burstBase, "a.log", "failure")
	n.add(burstBase.Add(30*time.Minute), "a.log", "failure")
	for i := 0; i < 15; i++ {
		n.add(burstBase.Add(15*time.Minute), "a.log", "failure")
	}
	assert.Empty(t, n.detect())
}

func TestNoBurstInExcerptOfOnlyTheBurst(t *testing.T) {
	for _, start := range []time.Time{burstBase.Add(5 * time.Minute), burstBase.Add(5*time.Minute - 5*time.Second)} {
		n := newNoteSet()
		n.addPaymentBurst(start)
		assert.Empty(t, n.detect(), "start %s", start)
	}
}

func TestNoBurstInSlowTrickle(t *testing.T) {
	n := newNoteSet()
	for s := 0; s < 3600; s += 3 {
		n.add(burstBase.Add(time.Duration(s)*time.Second), "a.log", "slow failure")
	}
	assert.Empty(t, n.detect())
}

func TestUndatedOccurrencesNeverJoinBursts(t *testing.T) {
	n := newNoteSet()
	n.addBackground()
	n.addPaymentBurst(burstBase.Add(5 * time.Minute))
	for i := 0; i < 50; i++ {
		n.add(time.Time{}, "same.log", "undated failure")
	}

	require.Len(t, n.detect(), 1)
	assert.False(t, n.spikeOf("undated failure"))
}

func TestBurstTopErrorsRanking(t *testing.T) {
	n := newNoteSet()
	n.addBackground()
	start := burstBase.Add(5 * time.Minute)
	i := 0
	for name, count := range map[string]int{"alpha": 200, "bravo": 150, "charlie": 50, "delta": 10} {
		for k := 0; k < count; k++ {
			n.add(start.Add(time.Duration(i)*25*time.Millisecond), "same.log", name+" failed")
			i++
		}
	}

	bursts := n.detect()

	require.Len(t, bursts, 1)
	require.Len(t, bursts[0].TopErrors, 3)
	var counts []int64
	for _, e := range bursts[0].TopErrors {
		counts = append(counts, e.Count)
	}
	assert.Equal(t, []int64{200, 150, 50}, counts)
}

func TestBurstsDoNotDependOnNoteOrder(t *testing.T) {
	n := newNoteSet()
	n.addBackground()
	n.addPaymentBurst(burstBase.Add(5 * time.Minute))
	want := n.detect()

	for i, j := 0, len(n.notes)-1; i < j; i, j = i+1, j-1 {
		n.notes[i], n.notes[j] = n.notes[j], n.notes[i]
	}
	assert.Equal(t, want, n.detect())
}

// quietSource adds one error at each end of an hour, so the source's median
// per-second count is 0.
func (n *noteSet) quietSource() {
	n.add(burstBase, "same.log", "heartbeat missed")
	n.add(burstBase.Add(time.Hour), "same.log", "heartbeat missed")
}

func (n *noteSet) addSecond(at time.Time, events int, msg string) {
	for i := 0; i < events; i++ {
		n.add(at.Add(time.Duration(i)*time.Millisecond), "same.log", msg)
	}
}

func TestBurstJoinsOneQuietSecondAndSplitsOnTwo(t *testing.T) {
	at := burstBase.Add(10 * time.Minute)
	tests := []struct {
		name       string
		gapSeconds int
		wantBursts int
	}{
		{"one quiet second joins two hot seconds", 1, 1},
		{"two quiet seconds split them", 2, 0}, // 15 + 15 apart: each run is under 20 events
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := newNoteSet()
			n.quietSource()
			n.addSecond(at, 15, "queue full")
			n.addSecond(at.Add(time.Duration(tt.gapSeconds+1)*time.Second), 15, "queue full")

			bursts := n.detect()

			require.Len(t, bursts, tt.wantBursts)
			if tt.wantBursts == 1 {
				assert.Equal(t, int64(30), bursts[0].Events)
				assert.True(t, at.Add(2*time.Second).Equal(bursts[0].End), "end: %s", bursts[0].End)
			}
		})
	}
}

func TestBurstOfExactlyMinEvents(t *testing.T) {
	n := newNoteSet()
	n.quietSource()
	n.addSecond(burstBase.Add(10*time.Minute), 20, "queue full")

	bursts := n.detect()

	require.Len(t, bursts, 1)
	assert.Equal(t, int64(20), bursts[0].Events)
}

// A storm of 8 errors a second for 5 minutes in a source that otherwise logs
// one error every 10 s is a burst, though no second reaches 10 events.
func TestBurstFromSlowStormInQuietSource(t *testing.T) {
	n := newNoteSet()
	for i := 0; i < 360; i++ {
		n.add(burstBase.Add(time.Duration(i)*10*time.Second), "same.log", fmt.Sprintf("cache miss for key user:%d", i))
	}
	stormStart := burstBase.Add(20 * time.Minute)
	for i := 0; i < 2400; i++ {
		n.add(stormStart.Add(time.Duration(i)*125*time.Millisecond), "same.log", fmt.Sprintf("upstream timeout for order %d", i))
	}

	bursts := n.detect()

	require.Len(t, bursts, 1)
	assert.True(t, stormStart.Equal(bursts[0].Start), "start: %s", bursts[0].Start)
	assert.True(t, stormStart.Add(299*time.Second).Equal(bursts[0].End), "end: %s", bursts[0].End)
	assert.Equal(t, int64(2430), bursts[0].Events) // 2,400 timeouts + 30 background errors inside
	assert.Equal(t, int64(2400), bursts[0].TopErrors[0].Count)
}

func TestBurstsArePerService(t *testing.T) {
	d := NewDedupFilter(4)
	var notes []occurrence
	add := func(at time.Time, service, msg string) {
		id := d.Add(types.LogEntry{Timestamp: at, Level: types.LevelError, Source: "agg.log", Service: service, Message: msg})
		notes = append(notes, occurrence{record: id, second: at.Unix()})
	}
	// search logs 5 errors a second for 10 minutes; payments is quiet but for
	// 200 errors in 10 seconds. Mixed together, payments' burst would be under
	// 10x search's rate; per service it stands out.
	for i := 0; i < 3000; i++ {
		add(burstBase.Add(time.Duration(i)*200*time.Millisecond), "search", fmt.Sprintf("index shard %d slow", i))
	}
	add(burstBase, "payments", "heartbeat")
	add(burstBase.Add(10*time.Minute), "payments", "heartbeat")
	for i := 0; i < 200; i++ {
		add(burstBase.Add(5*time.Minute+time.Duration(i)*50*time.Millisecond), "payments", fmt.Sprintf("card %d declined", i))
	}

	bursts := detectBursts(notes, d, 10, 20)

	require.Len(t, bursts, 1)
	assert.Equal(t, "agg.log", bursts[0].Source)
	assert.Equal(t, "payments", bursts[0].Service)
}
