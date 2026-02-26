// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clock

import (
	"testing"
	"time"
)

func TestRealClock_Now(t *testing.T) {
	c := RealClock{}
	now := c.Now()
	if now.IsZero() {
		t.Error("RealClock.Now() returned zero time")
	}
}

func TestRealClock_Sleep(t *testing.T) {
	c := RealClock{}
	start := time.Now()
	c.Sleep(10 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("RealClock.Sleep() slept for %v, want at least 10ms", elapsed)
	}
}

func TestRealClock_After(t *testing.T) {
	c := RealClock{}
	start := time.Now()
	<-c.After(10 * time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 10*time.Millisecond {
		t.Errorf("RealClock.After() waited for %v, want at least 10ms", elapsed)
	}
}

func TestRealClock_Jitter(t *testing.T) {
	c := RealClock{}
	base := 100.0
	for i := 0; i < 100; i++ {
		val := c.Jitter(base)
		if val < 90.0 || val > 110.0 {
			t.Errorf("RealClock.Jitter(%f) = %f, want range [90, 110]", base, val)
		}
	}
}
