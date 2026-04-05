// internal/pkg/telemetry/heartbeat_test.go
package telemetry

import (
	"context"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestStartHeartbeat(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("normal stop", func(t *testing.T) {
		hb := make(chan struct{}, 10)
		stop := StartHeartbeat(context.Background(), 10*time.Millisecond, hb)

		<-hb // wait for at least one tick
		stop()
		stop() // verify idempotency
	})

	t.Run("context cancellation", func(t *testing.T) {
		hb := make(chan struct{}, 10)
		ctx, cancel := context.WithCancel(context.Background())

		stop := StartHeartbeat(ctx, 10*time.Millisecond, hb)
		defer stop()
		<-hb
		cancel() // context cancellation should clean up the goroutine
	})

	t.Run("invalid interval prevents panic", func(t *testing.T) {
		hb := make(chan struct{}, 10)
		stop := StartHeartbeat(context.Background(), 0, hb)
		stop() // Should not panic, should return immediately
	})

	t.Run("nil channel prevents panic", func(t *testing.T) {
		stop := StartHeartbeat(context.Background(), 10*time.Millisecond, nil)
		stop() // Should not panic
	})
}
