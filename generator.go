package beakid

import (
	"runtime"
	"sync/atomic"
	"time"
)

const (
	timestampShift uint64 = 29
	timestampMask  uint64 = ^((uint64(1) << 29) - 1)
	workerIDMask   uint64 = (uint64(1) << 10) - 1
	incr           uint64 = uint64(1) << 10

	stateNotInited uint64 = 1
	stateBlocked   uint64 = 1 << 1
	stateUpdating  uint64 = 1 << 2
)

// Generator is a lock-free unique ID generator based on the Snowflake algorithm.
//
// Each ID is a 64-bit integer with the following layout:
//
//	[63..29] timestamp  — 35 bits, units of 100 ms since epoch
//	[28..10] sequence   — 19 bits, up to 524 288 IDs per window
//	[9..0]   worker_id  — 10 bits, distinguishes generator instances
//
// IDs are monotonically increasing within a single goroutine and unique across
// all goroutines sharing the same Generator instance.
//
// Generate calls UpdateTime automatically at every virtual window boundary
// (every 524 288 IDs), so the generator can advance and unblock itself under
// high load. A background goroutine calling UpdateTime every 100 ms is still
// recommended to keep the timestamp current at low load. Use Run to set this
// up automatically.
type Generator struct {
	id    atomic.Uint64
	state atomic.Uint64
	epoch time.Time
}

// TryNew creates a new Generator, returning an error if workerID is out of range.
//
//   - workerID must be in 0..=1023. Two generators running simultaneously must
//     have different workerIDs to guarantee globally unique IDs.
//   - epoch is the reference point for timestamps. Use a fixed date — changing it
//     invalidates sort order of existing IDs.
//
// The generator is not ready to produce IDs until UpdateTime is called at least
// once, or use Run which handles this automatically.
func TryNew(workerID uint64, epoch time.Time) (*Generator, error) {
	if workerID >= 1024 {
		return nil, ErrInvalidWorkerID
	}
	g := &Generator{epoch: epoch}
	g.id.Store(workerID)
	g.state.Store(stateNotInited)
	return g, nil
}

// MustNew creates a new Generator. Panics if workerID >= 1024.
func MustNew(workerID uint64, epoch time.Time) *Generator {
	g, err := TryNew(workerID, epoch)
	if err != nil {
		panic("beakid: worker_id out of range (>=1024)")
	}
	return g
}

// Generate attempts to produce a unique ID, returning ErrBlocked if all 10
// virtual time windows are exhausted.
//
// On success the returned BeakId is guaranteed to be unique across all
// concurrent callers sharing this Generator, and greater than any ID previously
// returned on the same goroutine.
//
// If UpdateTime is running concurrently, Generate spins with runtime.Gosched
// until the update finishes — this state lasts nanoseconds.
//
// When the sequence counter crosses a virtual window boundary (every 524 288
// IDs), Generate calls UpdateTime inline to check whether real time has caught
// up.
//
// Panics if UpdateTime has never been called (generator not initialised).
func (g *Generator) Generate() (BeakId, error) {
	var s uint64
	for {
		s = g.state.Load()
		if s&stateUpdating == 0 {
			break
		}
		runtime.Gosched()
	}

	if s == stateNotInited {
		panic("beakid: generator not initialised — call UpdateTime first, or use Run")
	}
	if s&stateBlocked != 0 {
		return 0, ErrBlocked
	}

	newID := g.id.Add(incr)
	oldID := newID - incr

	if (oldID & timestampMask) != (newID & timestampMask) {
		g.UpdateTime()
	}

	return BeakId(oldID), nil
}

// MustGenerate generates a unique ID, spinning until one is available.
//
// On ErrBlocked, calls UpdateTime and yields, allowing the generator to
// unblock once real time has advanced sufficiently. Suitable for synchronous
// contexts where a blocking spin is acceptable.
//
// Panics if UpdateTime has never been called (generator not initialised).
func (g *Generator) MustGenerate() BeakId {
	for {
		id, err := g.Generate()
		if err == nil {
			return id
		}
		g.UpdateTime()
		runtime.Gosched()
	}
}

// UpdateTime advances the internal timestamp and manages virtual window state.
//
// Must be called every 100 ms by a background goroutine for the generator to
// function correctly. The first call initialises the generator; subsequent calls
// keep the timestamp current.
//
// Virtual windows: each 100 ms window holds up to 524 288 IDs. If the sequence
// counter overflows into the next window before real time catches up, the
// generator borrows that window (up to 10 in total). If all 10 are exhausted,
// the generator transitions to ErrBlocked until a future call finds that real
// time has advanced far enough.
//
// Concurrency-safe: if two calls race, the second exits immediately without
// corrupting state.
//
// Panics if the current system time is earlier than epoch.
func (g *Generator) UpdateTime() {
	s := g.state.Load()
	for {
		if s&stateUpdating != 0 {
			return
		}

		if s&stateBlocked != 0 {
			ts := g.nowTimestamp()
			id := g.id.Load()
			if id > ts {
				delta := (id - ts) >> timestampShift
				if delta >= 8 {
					return
				}
			}
			g.state.Store(0)
			return
		}

		if g.state.CompareAndSwap(s, stateUpdating|stateBlocked) {
			break
		}
		s = g.state.Load()
	}

	ts := g.nowTimestamp()

	for {
		id := g.id.Load()

		if id > ts {
			delta := (id - ts) >> timestampShift
			if delta < 10 {
				break
			}
			g.state.Store(stateBlocked)
			return
		}

		newID := (id & workerIDMask) | ts
		if g.id.CompareAndSwap(id, newID) {
			break
		}
	}

	g.state.Store(0)
}

func (g *Generator) nowTimestamp() uint64 {
	d := time.Since(g.epoch)
	if d < 0 {
		panic("beakid: current time is before epoch")
	}
	return (uint64(d.Milliseconds()) / 100) << timestampShift
}
