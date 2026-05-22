// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestBuildWhereClause(t *testing.T) {
	refTime := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		filter     ports.ListFilter
		wantSQL    string
		wantArgLen int
	}{
		{
			name:       "empty filter produces no WHERE",
			filter:     ports.ListFilter{},
			wantSQL:    "",
			wantArgLen: 0,
		},
		{
			name:       "Status only",
			filter:     ports.ListFilter{Status: "pending"},
			wantSQL:    " WHERE status = ?",
			wantArgLen: 1,
		},
		{
			name:       "NotStatus only",
			filter:     ports.ListFilter{NotStatus: "completed"},
			wantSQL:    " WHERE status != ?",
			wantArgLen: 1,
		},
		{
			name:       "Since only",
			filter:     ports.ListFilter{Since: refTime},
			wantSQL:    " WHERE created_at >= ?",
			wantArgLen: 1,
		},
		{
			name:       "Before only",
			filter:     ports.ListFilter{Before: refTime},
			wantSQL:    " WHERE created_at <= ?",
			wantArgLen: 1,
		},
		{
			name:       "Status + Since",
			filter:     ports.ListFilter{Status: "pending", Since: refTime},
			wantSQL:    " WHERE status = ? AND created_at >= ?",
			wantArgLen: 2,
		},
		{
			name: "Status + NotStatus + Since + Before",
			filter: ports.ListFilter{
				Status:    "pending",
				NotStatus: "archived",
				Since:     refTime,
				Before:    refTime.Add(24 * time.Hour),
			},
			wantSQL:    " WHERE status = ? AND status != ? AND created_at >= ? AND created_at <= ?",
			wantArgLen: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wc := buildWhereClause(tt.filter)

			if wc.sql != tt.wantSQL {
				t.Errorf("sql = %q; want %q", wc.sql, tt.wantSQL)
			}
			if len(wc.args) != tt.wantArgLen {
				t.Errorf("args len = %d; want %d", len(wc.args), tt.wantArgLen)
			}

			// Verify args are in correct order matching the SQL ? placeholders
			argIdx := 0
			if tt.filter.Status != "" {
				if wc.args[argIdx] != tt.filter.Status {
					t.Errorf("args[%d] = %v; want Status %q", argIdx, wc.args[argIdx], tt.filter.Status)
				}
				argIdx++
			}
			if tt.filter.NotStatus != "" {
				if wc.args[argIdx] != tt.filter.NotStatus {
					t.Errorf("args[%d] = %v; want NotStatus %q", argIdx, wc.args[argIdx], tt.filter.NotStatus)
				}
				argIdx++
			}
			if !tt.filter.Since.IsZero() {
				if wc.args[argIdx] != tt.filter.Since.Format(time.RFC3339Nano) {
					t.Errorf("args[%d] = %v; want Since %q", argIdx, wc.args[argIdx], tt.filter.Since.Format(time.RFC3339Nano))
				}
				argIdx++
			}
			if !tt.filter.Before.IsZero() {
				if wc.args[argIdx] != tt.filter.Before.Format(time.RFC3339Nano) {
					t.Errorf("args[%d] = %v; want Before %q", argIdx, wc.args[argIdx], tt.filter.Before.Format(time.RFC3339Nano))
				}
			}
		})
	}
}
