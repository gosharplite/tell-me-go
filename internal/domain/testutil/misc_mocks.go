// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/mock"
)

// MockEntropySource is a mock implementation of io.Reader for entropy.
type MockEntropySource struct {
	mock.Mock
}

func (m *MockEntropySource) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	if args.Get(0) != nil {
		copy(p, args.Get(0).([]byte))
	}
	return args.Int(1), args.Error(2)
}

// MockEstimator is a mock implementation of TokenGatekeeper's tokenEstimator.
type MockEstimator struct {
	tokens int
}

func (m *MockEstimator) EstimateTokens(contents []*llm.Content) int {
	return m.tokens
}

func (m *MockEstimator) SetTokens(n int) {
	m.tokens = n
}

// Implement llm.TokenEstimator
func (m *MockEstimator) Count(contents []*llm.Content) int {
	return m.tokens
}

func (m *MockEstimator) CountTokens(text string) int {
	return m.tokens
}

// TestifyMockClock is a mock implementation of clock.Clock using testify/mock.
type TestifyMockClock struct {
	mock.Mock
}

func (m *TestifyMockClock) Now() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}

func (m *TestifyMockClock) Since(t time.Time) time.Duration {
	return m.Now().Sub(t)
}

func (m *TestifyMockClock) Sleep(d time.Duration) {
	m.Called(d)
}

func (m *TestifyMockClock) After(d time.Duration) <-chan time.Time {
	args := m.Called(d)
	return args.Get(0).(<-chan time.Time)
}

func (m *TestifyMockClock) NewTicker(d time.Duration) clock.Ticker {
	args := m.Called(d)
	return args.Get(0).(clock.Ticker)
}

func (m *TestifyMockClock) Jitter(base float64) float64 {
	args := m.Called(base)
	return args.Get(0).(float64)
}
