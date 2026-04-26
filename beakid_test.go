package beakid_test

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/tokime/beakid-go"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func makeGenerator(t *testing.T, workerID uint64) *beakid.Generator {
	t.Helper()
	g := beakid.MustNew(workerID, time.UnixMilli(0).UTC())
	g.UpdateTime()
	return g
}

func countDuplicates(ids []beakid.BeakId) int {
	sorted := make([]beakid.BeakId, len(ids))
	copy(sorted, ids)
	slices.Sort(sorted)
	dups := 0
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			dups++
		}
	}
	return dups
}

const idsPerWindow = 1 << 19 // 524_288

// ─── uniqueness ──────────────────────────────────────────────────────────────

func TestSingleThreadIdsAreUnique(t *testing.T) {
	g := makeGenerator(t, 0)

	ids := make([]beakid.BeakId, 0, 10_000)
	for range 10_000 {
		id, err := g.Generate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ids = append(ids, id)
	}

	if dups := countDuplicates(ids); dups != 0 {
		t.Errorf("got %d duplicates, want 0", dups)
	}
}

func TestConcurrentIdsAreUnique(t *testing.T) {
	g := makeGenerator(t, 0)

	var mu sync.Mutex
	var all []beakid.BeakId
	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			local := make([]beakid.BeakId, 10_000)
			for i := range 10_000 {
				local[i] = g.MustGenerate()
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		})
	}
	wg.Wait()

	if dups := countDuplicates(all); dups != 0 {
		t.Errorf("got %d duplicates across goroutines, want 0", dups)
	}
}

func TestWorkersWithDifferentIDsNeverCollide(t *testing.T) {
	g0 := makeGenerator(t, 0)
	g1 := makeGenerator(t, 1)

	ch0 := make(chan []beakid.BeakId, 1)
	ch1 := make(chan []beakid.BeakId, 1)

	go func() {
		ids := make([]beakid.BeakId, 10_000)
		for i := range 10_000 {
			ids[i] = g0.MustGenerate()
		}
		ch0 <- ids
	}()
	go func() {
		ids := make([]beakid.BeakId, 10_000)
		for i := range 10_000 {
			ids[i] = g1.MustGenerate()
		}
		ch1 <- ids
	}()

	all := append(<-ch0, <-ch1...)
	if dups := countDuplicates(all); dups != 0 {
		t.Errorf("got %d collisions between worker 0 and 1, want 0", dups)
	}
}

// ─── monotonicity ─────────────────────────────────────────────────────────────

func TestSingleGoroutineIdsAreMonotonicallyIncreasing(t *testing.T) {
	g := makeGenerator(t, 0)
	prev, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}

	for range 9_999 {
		cur, err := g.Generate()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cur <= prev {
			t.Fatalf("monotonicity broken: %v <= %v", cur, prev)
		}
		prev = cur
	}
}

func TestIdsRemainMonotonicAcrossUpdateTimeCalls(t *testing.T) {
	g := makeGenerator(t, 0)
	prev, _ := g.Generate()

	for range 5 {
		for range 1_000 {
			cur, err := g.Generate()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cur <= prev {
				t.Fatalf("monotonicity broken after UpdateTime: %v <= %v", cur, prev)
			}
			prev = cur
		}
		g.UpdateTime()
	}
}

// ─── virtual windows ──────────────────────────────────────────────────────────

func TestVirtualWindowsAllowBurstBeyondSingleWindow(t *testing.T) {
	g := makeGenerator(t, 0)

	target := idsPerWindow + 10_000
	generated := 0
	for range target {
		_, err := g.Generate()
		if err == nil {
			generated++
		}
	}

	if generated < idsPerWindow {
		t.Errorf("virtual windows should allow at least %d IDs, got %d", idsPerWindow, generated)
	}
}

func TestIdsUniqueAcrossVirtualWindowBoundaries(t *testing.T) {
	g := makeGenerator(t, 0)

	target := idsPerWindow * 3
	ids := make([]beakid.BeakId, 0, target)
	for range target {
		id, err := g.Generate()
		if err == beakid.ErrBlocked {
			break
		}
		if err == nil {
			ids = append(ids, id)
		}
	}

	if dups := countDuplicates(ids); dups != 0 {
		t.Errorf("got %d duplicates across virtual window boundaries, want 0", dups)
	}
}

// ─── blocking / unblocking ────────────────────────────────────────────────────

func makeFreshGenerator() *beakid.Generator {
	g := beakid.MustNew(0, time.Now())
	g.UpdateTime()
	return g
}

func tryExhaustAndBlock(g *beakid.Generator) bool {
	total := idsPerWindow * 20
	nThreads := 64
	perThread := (total + nThreads - 1) / nThreads

	var wg sync.WaitGroup
	for range nThreads {
		wg.Go(func() {
			for range perThread {
				g.Generate() //nolint:errcheck
			}
		})
	}
	wg.Wait()

	g.UpdateTime()
	_, err := g.Generate()
	return err == beakid.ErrBlocked
}

func TestBlocksWhenVirtualWindowsExhausted(t *testing.T) {
	g := makeFreshGenerator()
	blocked := tryExhaustAndBlock(g)
	if !blocked {
		t.Skip("machine too slow to exhaust windows within one 100ms tick")
	}

	_, err := g.Generate()
	if err != beakid.ErrBlocked {
		t.Errorf("expected ErrBlocked after all virtual windows exhausted, got %v", err)
	}
}

func TestUnblocksAfterTimeAdvances(t *testing.T) {
	g := makeFreshGenerator()
	blocked := tryExhaustAndBlock(g)
	if !blocked {
		t.Skip("could not reach blocked state on this machine")
	}

	time.Sleep(2200 * time.Millisecond)
	g.UpdateTime()

	_, err := g.Generate()
	if err != nil {
		t.Errorf("expected generator to unblock after real time advances, got %v", err)
	}
}

// ─── base62 ──────────────────────────────────────────────────────────────────

func TestBase62RoundtripPreservesID(t *testing.T) {
	g := makeGenerator(t, 0)
	for range 1_000 {
		id, _ := g.Generate()
		decoded, err := beakid.FromBase62(id.Base62())
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if decoded != id {
			t.Fatalf("roundtrip mismatch: got %v, want %v", decoded, id)
		}
	}
}

func TestBase62IsAlways11Chars(t *testing.T) {
	cases := []uint64{0, 1, 1_000_000, ^uint64(0) / 2, ^uint64(0)}
	for _, v := range cases {
		s := beakid.New(v).Base62()
		if len(s) != 11 {
			t.Errorf("Base62(%d) = %q, len=%d, want 11", v, s, len(s))
		}
	}
}

func TestFromBase62RejectsWrongLength(t *testing.T) {
	for _, s := range []string{"", "0000000000", "000000000000"} {
		if _, err := beakid.FromBase62(s); err == nil {
			t.Errorf("FromBase62(%q) should fail", s)
		}
	}
}

func TestFromBase62RejectsInvalidChars(t *testing.T) {
	for _, s := range []string{"0000000000!", "0000000000 ", "0000000000\n"} {
		if _, err := beakid.FromBase62(s); err == nil {
			t.Errorf("FromBase62(%q) should fail", s)
		}
	}
}

// ─── constructor ─────────────────────────────────────────────────────────────

func TestTryNewValidatesWorkerIDBounds(t *testing.T) {
	epoch := time.UnixMilli(0).UTC()

	if _, err := beakid.TryNew(0, epoch); err != nil {
		t.Errorf("TryNew(0) unexpected error: %v", err)
	}
	if _, err := beakid.TryNew(1023, epoch); err != nil {
		t.Errorf("TryNew(1023) unexpected error: %v", err)
	}
	if _, err := beakid.TryNew(1024, epoch); err == nil {
		t.Error("TryNew(1024) should return error")
	}
	if _, err := beakid.TryNew(^uint64(0), epoch); err == nil {
		t.Error("TryNew(MaxUint64) should return error")
	}
}
