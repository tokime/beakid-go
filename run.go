package beakid

import (
	"context"
	"time"
)

// Run creates a Generator, calls UpdateTime immediately to initialise it, and
// starts a background goroutine that calls UpdateTime every 100 ms.
//
// Call the returned stop function to cancel the background goroutine. Dropping
// the stop function without calling it leaks the goroutine.
//
// This is the Go equivalent of the tokio_run! / smol_run! macros from the Rust
// version of this library.
func Run(workerID uint64, epoch time.Time) (*Generator, context.CancelFunc, error) {
	g, err := TryNew(workerID, epoch)
	if err != nil {
		return nil, nil, err
	}
	g.UpdateTime()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				g.UpdateTime()
			case <-ctx.Done():
				return
			}
		}
	}()

	return g, cancel, nil
}
