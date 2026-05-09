// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clock

import (
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	t.Parallel()
	c := RealClock{}
	now := c.Now()
	if now.IsZero() {
		t.Error("RealClock.Now() returned zero time")
	}
}

func TestRealClock_Sleep(t *testing.T) {
	t.Parallel()
	c := RealClock{}
	start := time.Now()
	c.Sleep(10 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("RealClock.Sleep() slept for %v, want at least 10ms", elapsed)
	}
}

func TestRealClock_After(t *testing.T) {
	t.Parallel()
	c := RealClock{}
	start := time.Now()
	<-c.After(10 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("RealClock.After() waited for %v, want at least 10ms", elapsed)
	}
}

func TestRealClock_Jitter(t *testing.T) {
	t.Parallel()
	c := RealClock{}
	base := 100.0
	for i := 0; i < 100; i++ {
		val := c.Jitter(base)
		if val < 90.0 || val > 110.0 {
			t.Errorf("RealClock.Jitter(%f) = %f, want range [90, 110]", base, val)
		}
	}
}

// TestRealClock_Since verifies that RealClock.Since returns a non-negative duration
// that is at least as long as a known sleep interval.
func TestRealClock_Since(t *testing.T) {
	t.Parallel()
	c := RealClock{}
	t0 := c.Now()
	c.Sleep(10 * time.Millisecond)
	d := c.Since(t0)

	if d < 0 {
		t.Errorf("RealClock.Since() = %v, want non-negative", d)
	}
	if d < 10*time.Millisecond {
		t.Errorf("RealClock.Since() = %v, want at least 10ms", d)
	}
}

// TestRealTicker_C verifies that realTicker.C returns a non-nil channel and that
// repeated calls return the same underlying channel.
func TestRealTicker_C(t *testing.T) {
	c := RealClock{}
	tk := c.NewTicker(time.Hour)
	t.Cleanup(tk.Stop)

	ch1 := tk.C()
	ch2 := tk.C()

	if ch1 == nil {
		t.Error("realTicker.C() returned nil channel")
	}
	if ch1 != ch2 {
		t.Error("realTicker.C() returned different channels across calls — expected same channel")
	}
}

// TestRealClock_NewTicker verifies that RealClock.NewTicker returns a Ticker
// whose channel fires at least once within a timeout window.
func TestRealClock_NewTicker(t *testing.T) {
	c := RealClock{}
	tk := c.NewTicker(10 * time.Millisecond)
	t.Cleanup(tk.Stop)

	select {
	case tick := <-tk.C():
		if tick.IsZero() {
			t.Error("NewTicker channel delivered zero time")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("NewTicker did not fire within 200ms timeout")
	}
}
