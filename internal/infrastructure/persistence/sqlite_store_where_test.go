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

			verifyWhereArgs(t, wc, tt.filter)
		})
	}
}

// verifyWhereArgs checks that wc.args contains the correct values matching
// the non-zero fields of f, in the same order that buildWhereClause places them.
func verifyWhereArgs(t *testing.T, wc whereClause, f ports.ListFilter) {
	t.Helper()

	argIdx := 0
	checkArg(t, wc.args, &argIdx, "Status", f.Status, f.Status != "")
	checkArg(t, wc.args, &argIdx, "NotStatus", f.NotStatus, f.NotStatus != "")
	checkArg(t, wc.args, &argIdx, "Since", f.Since.Format(time.RFC3339Nano), !f.Since.IsZero())
	checkArg(t, wc.args, &argIdx, "Before", f.Before.Format(time.RFC3339Nano), !f.Before.IsZero())
}

// checkArg verifies a single argument at args[*idx] if isSet is true,
// then advances idx. Skips verification when isSet is false.
func checkArg(t *testing.T, args []any, idx *int, fieldName, want string, isSet bool) {
	t.Helper()
	if !isSet {
		return
	}
	if args[*idx] != want {
		t.Errorf("args[%d] = %v; want %s %q", *idx, args[*idx], fieldName, want)
	}
	*idx++
}
