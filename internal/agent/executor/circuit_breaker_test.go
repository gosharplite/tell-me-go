// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestFailureTracker_Boundaries(t *testing.T) {
	t.Parallel()
	tool := "test-tool"
	threshold := 3

	tests := []struct {
		name    string
		results []bool // false = failure, true = success
		wantErr bool
	}{
		{
			name:    "Case 1 (Initial Failure): 1 failure -> State remains Closed",
			results: []bool{false},
			wantErr: false,
		},
		{
			name:    "Case 2 (Just Below Threshold): Threshold - 1 consecutive failures -> State remains Closed",
			results: []bool{false, false},
			wantErr: false,
		},
		{
			name:    "Case 3 (Trip Breaker): Threshold consecutive failures -> State transitions to Open",
			results: []bool{false, false, false},
			wantErr: true,
		},
		{
			name:    "Case 4 (Reset on Success): N < Threshold failures followed by 1 success -> Resets counter to 0",
			results: []bool{false, false, true, false, false},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt // Explicitly capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ft := newFailureTracker(threshold)
			for _, success := range tt.results {
				ft.Record(tool, success)
			}
			err := ft.Check(tool)
			if tt.wantErr {
				assert.Error(t, err)
				assert.ErrorIs(t, err, tools.ErrToolCircuitOpen)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
