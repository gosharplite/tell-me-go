// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"time"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

type mockClock struct {
	clock.Clock
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) Sleep(d time.Duration) {}
func (m *mockClock) After(d time.Duration) <-chan time.Time { return nil }
func (m *mockClock) Jitter(base float64) float64 { return base }
