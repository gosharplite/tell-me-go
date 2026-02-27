// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

// ThresholdStrategy defines the interface for evaluating if a token threshold has been reached.
type ThresholdStrategy interface {
	Evaluate(tokens int) bool
	GetThreshold() int
}

// FreeTierStrategy implements a threshold strategy for the free tier.
type FreeTierStrategy struct {
	Threshold int
}

// Evaluate checks if tokens exceed the free tier threshold.
func (s *FreeTierStrategy) Evaluate(tokens int) bool {
	return s.Threshold > 0 && tokens > s.Threshold
}

// GetThreshold returns the free tier threshold.
func (s *FreeTierStrategy) GetThreshold() int {
	return s.Threshold
}

// ProTierStrategy implements a threshold strategy for the pro tier.
type ProTierStrategy struct {
	Threshold int
}

// Evaluate checks if tokens exceed the pro tier threshold.
func (s *ProTierStrategy) Evaluate(tokens int) bool {
	return s.Threshold > 0 && tokens > s.Threshold
}

// GetThreshold returns the pro tier threshold.
func (s *ProTierStrategy) GetThreshold() int {
	return s.Threshold
}

// DynamicThresholdStrategy implements a threshold strategy that delegates to the estimator.
type DynamicThresholdStrategy struct {
	Estimator tokenEstimator
}

// Evaluate checks if tokens exceed the dynamic threshold from the estimator.
func (s *DynamicThresholdStrategy) Evaluate(tokens int) bool {
	if cs, ok := s.Estimator.(*ContextStrategy); ok {
		tiered := cs.GetTieredThreshold()
		return tiered > 0 && tokens > tiered
	}
	return false
}

// GetThreshold returns the dynamic threshold from the estimator.
func (s *DynamicThresholdStrategy) GetThreshold() int {
	if cs, ok := s.Estimator.(*ContextStrategy); ok {
		return cs.GetTieredThreshold()
	}
	return 0
}
