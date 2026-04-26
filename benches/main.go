package main

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tokime/beakid-go"
)

func main() {
	fmt.Println("beakid go generator benchmark")
	fmt.Println()
	fmt.Printf("%-14s %6s %12s %14s %10s %9s %12s\n",
		"case", "gorout", "total ids", "ids/s", "ns/id", "virt-win", "unique")
	fmt.Println(strings.Repeat("-", 81))

	benchRun(1, 100_000)
	benchRun(2, 200_000)
	benchRun(4, 400_000)
	benchRun(8, 1_000_000)
	benchRun(28, 28_000_000)
	benchRun(56, 56_000_000)
}

func benchRun(goroutines int, totalIDs uint64) {
	spin := runCase("spin", goroutines, totalIDs, false)
	printResult(spin)

	gosched := runCase("gosched", goroutines, totalIDs, true)
	printResult(gosched)

	if spin.duplicates > 0 || gosched.duplicates > 0 {
		fmt.Fprintln(os.Stderr, "\nFAIL: duplicate IDs detected")
		os.Exit(1)
	}
}

func printResult(r benchResult) {
	uniqueCol := "ok"
	if r.duplicates > 0 {
		uniqueCol = fmt.Sprintf("FAIL (%d dup)", r.duplicates)
	}
	fmt.Printf("%-14s %6d %12d %14.0f %10.2f %9d %12s\n",
		r.name, r.goroutines, r.totalIDs,
		r.idsPerSecond(), r.nsPerID(), r.virtualWindowsUsed(),
		uniqueCol)
}

type benchResult struct {
	name       string
	goroutines int
	totalIDs   uint64
	elapsed    time.Duration
	duplicates uint64
}

func (r benchResult) idsPerSecond() float64 {
	return float64(r.totalIDs) / r.elapsed.Seconds()
}

func (r benchResult) nsPerID() float64 {
	return float64(r.elapsed.Nanoseconds()) / float64(r.totalIDs)
}

// virtualWindowsUsed returns how many virtual windows the generator consumed
// beyond what real time provided — positive means the generator ran faster
// than the clock.
func (r benchResult) virtualWindowsUsed() uint64 {
	const idsPerWindow = uint64(1 << 19)
	windowsConsumed := r.totalIDs / idsPerWindow
	realWindows := uint64(r.elapsed.Milliseconds()) / 100
	if windowsConsumed < realWindows {
		return 0
	}
	return windowsConsumed - realWindows
}

func runCase(name string, goroutines int, totalIDs uint64, gosched bool) benchResult {
	generator, stop, err := beakid.Run(0, time.UnixMilli(0).UTC())
	if err != nil {
		panic(err)
	}
	defer stop()

	// Let the background ticker fire at least once.
	time.Sleep(time.Millisecond)

	opsPerGoroutine := totalIDs / uint64(goroutines)

	var (
		mu     sync.Mutex
		allIDs []beakid.BeakId
		wg     sync.WaitGroup
	)
	allIDs = make([]beakid.BeakId, 0, int(totalIDs))

	start := time.Now()

	for range goroutines {
		wg.Go(func() {
			ids := generateMany(generator, opsPerGoroutine, gosched)
			mu.Lock()
			allIDs = append(allIDs, ids...)
			mu.Unlock()
		})
	}
	wg.Wait()

	elapsed := time.Since(start)
	collected := uint64(len(allIDs))

	slices.Sort(allIDs)
	var dups uint64
	for i := 1; i < len(allIDs); i++ {
		if allIDs[i] == allIDs[i-1] {
			dups++
		}
	}

	return benchResult{
		name:       name,
		goroutines: goroutines,
		totalIDs:   collected,
		elapsed:    elapsed,
		duplicates: dups,
	}
}

func generateMany(g *beakid.Generator, n uint64, gosched bool) []beakid.BeakId {
	ids := make([]beakid.BeakId, 0, int(n))

	if gosched {
		for range n {
			var id beakid.BeakId
			for {
				var err error
				id, err = g.Generate()
				if err == nil {
					break
				}
				runtime.Gosched()
			}
			ids = append(ids, id)
		}
	} else {
		for range n {
			ids = append(ids, g.MustGenerate())
		}
	}

	return ids
}
