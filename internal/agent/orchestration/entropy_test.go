// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_SessionID_DegradationWarning(t *testing.T) {
	mChatter := new(mockChatter)
	mCapturer := new(mockCapturer)
	mHistory := new(mockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
	inframock.CleanupBus(t, mEventBus)

	mClock := new(mockClock)
	mEntropy := new(mockEntropySource)

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	mClock.On("Now").Return(fixedTime)

	entropyErr := fmt.Errorf("OS entropy exhaustion")
	mEntropy.On("Read", mock.Anything).Return(nil, 0, entropyErr)

	var stderr bytes.Buffer

	factory := func(ctx context.Context, deps ports.SessionDependencies, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(mockHistoryRenderer)
	mUIRenderer := new(mockUIRenderer)
	orch := newOrchestrator("home", "1.0.0", nil, nil, io.Discard, &stderr, factory, mHistoryRenderer, mUIRenderer, mClock, mEntropy)

	sCfg := newSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := newSessionDependencies(&persistence.Paths{}, mHistory, nil, nil, nil, nil, nil, domain_pricing.PricingData{}, nil, mEventBus, slog.Default())

	mCapturer.On("IsTTY", io.Discard).Return(true)
	mUIRenderer.On("SetUseColor", true).Return()
	mChatter.On("Subscribe", mock.Anything).Return()
	mChatter.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	mChatter.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
	mChatter.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
	mChatter.On("Shutdown", mock.Anything).Return(nil)

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	assert.Contains(t, stderr.String(), "[WARN] Entropy source failure, degrading to time-based session ID: OS entropy exhaustion")
}
