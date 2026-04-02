package clock

import (
	"time"
)

// MockClock is a Clock implementation for testing.
type MockClock struct {
	currentTime time.Time
}

func NewMockClock(start time.Time) *MockClock {
	return &MockClock{
		currentTime: start,
	}
}

func (m *MockClock) Now() time.Time {
	return m.currentTime
}

func (m *MockClock) Since(t time.Time) time.Duration {
	return m.currentTime.Sub(t)
}

func (m *MockClock) Sleep(d time.Duration) {
	m.Advance(d)
}

func (m *MockClock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	// For a simple mock, we just return the time immediately advanced by d,
	// or rely on a more complex priority queue if needed. Here we keep it simple.
	ch <- m.currentTime.Add(d)
	return ch
}

func (m *MockClock) NewTicker(d time.Duration) Ticker {
	// A naive ticker for mock, which just ticks once at the mock current time + d.
	return &mockTicker{
		c:    make(chan time.Time),
		stop: make(chan struct{}),
	}
}

func (m *MockClock) Jitter(base float64) float64 {
	return base
}

func (m *MockClock) Advance(d time.Duration) {
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
