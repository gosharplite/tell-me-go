// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"
	"testing"
	"time"
)

// =============================================================================
// MockClock: SetCurrentTime + CurrentTime
// =============================================================================

func TestMockClock_SetCurrentTime_CurrentTime_RoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("set and get specific time", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		want := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
		m.SetCurrentTime(want)
		got := m.CurrentTime()
		if !got.Equal(want) {
			t.Errorf("CurrentTime() = %v; want %v", got, want)
		}
	})

	t.Run("zero time after init", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		got := m.CurrentTime()
		if !got.IsZero() {
			t.Errorf("CurrentTime() = %v; want zero time", got)
		}
	})

	t.Run("set to zero explicitly", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		m.SetCurrentTime(time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC))
		m.SetCurrentTime(time.Time{}) // explicitly zero
		got := m.CurrentTime()
		if !got.IsZero() {
			t.Errorf("CurrentTime() after explicit zero set = %v; want zero time", got)
		}
	})

	t.Run("multiple set/get cycles", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		times := []time.Time{
			time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			time.Date(2027, 12, 31, 23, 59, 59, 0, time.UTC),
			time.Time{}, // zero
		}
		for _, want := range times {
			m.SetCurrentTime(want)
			got := m.CurrentTime()
			if !got.Equal(want) {
				t.Errorf("CurrentTime() = %v; want %v", got, want)
			}
		}
	})

	t.Run("zero CurrentTime does NOT fall through to wall clock", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		// CurrentTime() returns the stored value (zero), not wall clock.
		// Only Now() has the fall-through logic.
		got := m.CurrentTime()
		if !got.IsZero() {
			t.Fatalf("CurrentTime() = %v; expected zero — getter bypasses wall-clock fallback", got)
		}
		// Confirm wall clock is NOT zero.
		wallNow := time.Now()
		if wallNow.IsZero() {
			t.Fatal("wall clock returned zero — test cannot validate the invariant")
		}
	})
}

// TestMockClock_SetCurrentTime_Concurrency ensures that concurrent calls
// to SetCurrentTime and CurrentTime do not trigger the race detector.
// This test must NOT use t.Parallel() because it depends on the shared
// mock and must not interfere with other tests.
func TestMockClock_SetCurrentTime_Concurrency(t *testing.T) {
	m := &MockClock{}

	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 50

	// Writers: call SetCurrentTime concurrently.
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.SetCurrentTime(time.Unix(int64(id*iterations+i), 0))
			}
		}(g)
	}

	// Readers: call CurrentTime concurrently.
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_ = m.CurrentTime()
			}
		}()
	}

	wg.Wait()

	// After all goroutines finish, the stored time should be readable
	// without racing.
	got := m.CurrentTime()
	_ = got // merely checking no panic/race
}

// =============================================================================
// MockClock: Since
// =============================================================================

func TestMockClock_Since(t *testing.T) {
	t.Parallel()

	t.Run("Since delegates to Now", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		fixed := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
		earlier := time.Date(2026, 3, 10, 13, 55, 0, 0, time.UTC)

		m.SetCurrentTime(fixed)
		got := m.Since(earlier)
		want := fixed.Sub(earlier) // 5 minutes
		if got != want {
			t.Errorf("Since(%v) = %v; want %v", earlier, got, want)
		}
	})

	t.Run("Since with zero stored time falls through to wall clock", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		// When stored time is zero, Now() falls through to wall clock.
		// Since calls Now(), so it also falls through.
		ref := time.Now().Add(-1 * time.Second)
		got := m.Since(ref)

		// Should be approximately 1 second (wall-clock elapsed).
		if got <= 0 {
			t.Errorf("Since(%v) = %v; expected positive wall-clock duration", ref, got)
		}
		if got > 5*time.Second {
			t.Errorf("Since(%v) = %v; expected ~1s, got unexpectedly large", ref, got)
		}
	})

	t.Run("Since with exact same time returns zero", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		fixed := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
		m.SetCurrentTime(fixed)
		got := m.Since(fixed)
		if got != 0 {
			t.Errorf("Since(same time) = %v; want 0", got)
		}
	})

	t.Run("Since with future time returns negative", func(t *testing.T) {
		t.Parallel()

		m := &MockClock{}
		fixed := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
		future := time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC)
		m.SetCurrentTime(fixed)
		got := m.Since(future)
		if got >= 0 {
			t.Errorf("Since(future) = %v; expected negative duration", got)
		}
	})
}

// =============================================================================
// MockTicker: C + Stop
// =============================================================================

func TestMockTicker_C(t *testing.T) {
	t.Parallel()

	known := make(chan time.Time, 1)
	mt := &MockTicker{CVal: known}
	got := mt.C()
	if got != known {
		t.Errorf("C() returned %v; want the known channel %v", got, known)
	}
}

func TestMockTicker_C_NilChannel(t *testing.T) {
	t.Parallel()

	mt := &MockTicker{} // CVal is nil (zero value)
	got := mt.C()
	if got != nil {
		t.Errorf("C() with nil CVal = %v; want nil", got)
	}
}

func TestMockTicker_Stop(t *testing.T) {
	t.Parallel()

	t.Run("Stop is no-op, does not panic", func(t *testing.T) {
		t.Parallel()

		mt := &MockTicker{}
		// Must not panic.
		mt.Stop()
	})

	t.Run("Stop on nil CVal does not panic", func(t *testing.T) {
		t.Parallel()

		mt := &MockTicker{CVal: nil}
		mt.Stop()
	})

	t.Run("Stop multiple times does not panic", func(t *testing.T) {
		t.Parallel()

		ch := make(chan time.Time, 1)
		mt := &MockTicker{CVal: ch}
		for i := 0; i < 5; i++ {
			mt.Stop()
		}
	})
}

// =============================================================================
// MockClock.NewTicker integration
// =============================================================================

func TestMockClock_NewTicker_Integration(t *testing.T) {
	t.Parallel()

	m := &MockClock{}
	fixed := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	m.SetCurrentTime(fixed)

	ticker := m.NewTicker(10 * time.Millisecond)
	mt, ok := ticker.(*MockTicker)
	if !ok {
		t.Fatalf("NewTicker returned %T; want *MockTicker", ticker)
	}

	// The C channel should receive the pre-loaded time (fixed + 10ms).
	select {
	case got := <-mt.C():
		want := fixed.Add(10 * time.Millisecond)
		if !got.Equal(want) {
			t.Errorf("received %v from ticker C; want %v", got, want)
		}
	default:
		t.Fatal("expected a value on the ticker channel, but none was available")
	}
}
