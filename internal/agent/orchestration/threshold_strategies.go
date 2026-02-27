// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

// ThresholdStrategy defines the interface for evaluating if a token threshold has been reached.
type ThresholdStrategy interface {
	Evaluate(tokens int) bool
	GetThreshold() int
}

// dynamicThresholdStrategy implements a threshold strategy that delegates to the estimator.
type dynamicThresholdStrategy struct {
	Estimator tokenEstimator
}

// Evaluate checks if tokens exceed the dynamic threshold from the estimator.
func (s *dynamicThresholdStrategy) Evaluate(tokens int) bool {
	if cs, ok := s.Estimator.(*ContextStrategy); ok {
		tiered := cs.GetTieredThreshold()
		return tiered > 0 && tokens > tiered
	}
	return false
}

// GetThreshold returns the dynamic threshold from the estimator.
func (s *dynamicThresholdStrategy) GetThreshold() int {
	if cs, ok := s.Estimator.(*ContextStrategy); ok {
		return cs.GetTieredThreshold()
	}
	return 0
}
