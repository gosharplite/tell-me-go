package telemetry

import (
	"context"
	"sync"
	"time"
)

// StartHeartbeat sends a periodic signal to the hb channel until the context is canceled
// or the returned stop function is called.
func StartHeartbeat(ctx context.Context, interval time.Duration, hb chan<- struct{}) (stop func()) {
	if hb == nil || interval <= 0 {
		return func() {} // No-op to prevent panic on invalid interval
	}

	done := make(chan struct{})
 go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				select {
				case hb <- struct{}{}:
				default: // Prevent blocking if channel is full
				}
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
		})
	}
}
