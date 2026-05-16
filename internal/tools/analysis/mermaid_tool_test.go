// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"strings"
	"testing"
)

func TestCoerceGraphMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input map[string]interface{}
		want  map[string][]string
	}{
		{
			name:  "empty map",
			input: map[string]interface{}{},
			want:  map[string][]string{},
		},
		{
			name: "[]string values",
			input: map[string]interface{}{
				"pkg1": []string{"pkg2", "pkg3"},
			},
			want: map[string][]string{
				"pkg1": {"pkg2", "pkg3"},
			},
		},
		{
			name: "[]interface{} values",
			input: map[string]interface{}{
				"pkg1": []interface{}{"pkg2", "pkg3"},
			},
			want: map[string][]string{
				"pkg1": {"pkg2", "pkg3"},
			},
		},
		{
			name: "mixed with non-string in []interface{}",
			input: map[string]interface{}{
				"pkg1": []interface{}{"pkg2", 42},
			},
			want: map[string][]string{
				"pkg1": {"pkg2"}, // 42 is skipped
			},
		},
		{
			name: "unknown value type skipped",
			input: map[string]interface{}{
				"pkg1": 42,
			},
			want: map[string][]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := coerceGraphMap(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
			}
			for k, wantV := range tt.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("missing key %q", k)
					continue
				}
				if len(gotV) != len(wantV) {
					t.Errorf("key %q: len = %d, want %d", k, len(gotV), len(wantV))
				}
			}
		})
	}
}

func TestNormalizeGraphArg(t *testing.T) {
	t.Parallel()
	t.Run("nil returns error", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeGraphArg(nil)
		if err == nil || err.Error() != "missing 'graph' argument" {
			t.Errorf("expected 'missing graph' error, got: %v", err)
		}
	})
	t.Run("map[string][]string passed through", func(t *testing.T) {
		t.Parallel()
		input := map[string][]string{"a": {"b"}}
		got, err := normalizeGraphArg(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got["a"][0] != "b" {
			t.Errorf("unexpected result: %v", got)
		}
	})
	t.Run("map[string]interface{} coerced", func(t *testing.T) {
		t.Parallel()
		input := map[string]interface{}{"a": []string{"b"}}
		got, err := normalizeGraphArg(input)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got["a"][0] != "b" {
			t.Errorf("unexpected result: %v", got)
		}
	})
	t.Run("wrong type returns error", func(t *testing.T) {
		t.Parallel()
		_, err := normalizeGraphArg("not a map")
		if err == nil {
			t.Error("expected error for wrong type")
		}
	})
}

func TestGenerateMermaidDiagram(t *testing.T) {
	t.Parallel()
	t.Run("missing graph key", func(t *testing.T) {
		t.Parallel()
		result, err := generateMermaidDiagram(context.Background(), map[string]interface{}{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Text != "Error: missing 'graph' argument" {
			t.Errorf("expected error text, got: %s", result.Text)
		}
	})
	t.Run("valid graph", func(t *testing.T) {
		t.Parallel()
		graph := map[string][]string{"pkg1": {"pkg2"}}
		result, err := generateMermaidDiagram(context.Background(), map[string]interface{}{"graph": graph}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Text == "" {
			t.Error("expected non-empty result")
		}
	})
	t.Run("wrong graph type", func(t *testing.T) {
		t.Parallel()
		result, err := generateMermaidDiagram(context.Background(), map[string]interface{}{"graph": "invalid"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Text, "Error:") {
			t.Errorf("expected error text, got: %s", result.Text)
		}
	})
}
