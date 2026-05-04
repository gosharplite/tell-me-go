// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"strings"
	"testing"
)

func TestLimits_Validate(t *testing.T) {
	tests := []struct {
		name      string
		limits    Limits
		wantErr   bool
		errSubstr string
	}{
		{
			name:   "all zero (defaults sentinel)",
			limits: Limits{},
		},
		{
			name:   "all positive",
			limits: Limits{MaxHistoryTokens: 8000, MaxToolTurns: 10, MaxHistoryTurns: 50},
		},
		{
			name:      "negative MaxHistoryTokens",
			limits:    Limits{MaxHistoryTokens: -1},
			wantErr:   true,
			errSubstr: "max history tokens",
		},
		{
			name:      "negative MaxToolTurns",
			limits:    Limits{MaxToolTurns: -1},
			wantErr:   true,
			errSubstr: "max tool turns",
		},
		{
			name:      "negative MaxHistoryTurns",
			limits:    Limits{MaxHistoryTurns: -1},
			wantErr:   true,
			errSubstr: "max history turns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.limits.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error = %q; want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
