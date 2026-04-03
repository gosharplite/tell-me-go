package executor

import (
	"time"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// mockClock is a Clock implementation for testing.
type mockClock struct {
	currentTime time.Time
}

func newMockClock(start time.Time) *mockClock {
	return &mockClock{
		currentTime: start,
	}
}

func (m *mockClock) Now() time.Time {
	return m.currentTime
}

func (m *mockClock) Since(t time.Time) time.Duration {
	return m.currentTime.Sub(t)
}

func (m *mockClock) Sleep(d time.Duration) {
	m.Advance(d)
}

func (m *mockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- m.currentTime.Add(d)
	return ch
}

func (m *mockClock) NewTicker(d time.Duration) clock.Ticker {
	return &mockTicker{
		c:    make(chan time.Time),
		stop: make(chan struct{}),
	}
}

func (m *mockClock) Jitter(base float64) float64 {
	return base
}

func (m *mockClock) Advance(d time.Duration) {
	m.currentTime = m.currentTime.Add(d)
}

type mockTicker struct {
	c    chan time.Time
	stop chan struct{}
}

func (mt *mockTicker) C() <-chan time.Time {
	return mt.c
}

func (mt *mockTicker) Stop() {
	close(mt.stop)
}
