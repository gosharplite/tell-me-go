// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
)

func TestUnmarshalArgs(t *testing.T) {
	t.Parallel()

	type testStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	tests := []struct {
		name    string
		args    map[string]interface{}
		target  interface{}
		wantErr bool
		verify  func(t *testing.T, target interface{})
	}{
		{
			name: "valid conversion",
			args: map[string]interface{}{
				"name":  "test",
				"value": 123,
			},
			target:  &testStruct{},
			wantErr: false,
			verify: func(t *testing.T, target interface{}) {
				ts := target.(*testStruct)
				if ts.Name != "test" || ts.Value != 123 {
					t.Errorf("unexpected values: %+v", ts)
				}
			},
		},
		{
			name: "partial values",
			args: map[string]interface{}{
				"name": "test",
			},
			target:  &testStruct{},
			wantErr: false,
			verify: func(t *testing.T, target interface{}) {
				ts := target.(*testStruct)
				if ts.Name != "test" || ts.Value != 0 {
					t.Errorf("unexpected values: %+v", ts)
				}
			},
		},
		{
			name: "type mismatch",
			args: map[string]interface{}{
				"value": "not-an-int",
			},
			target:  &testStruct{},
			wantErr: true,
		},
		{
			name:    "nil map",
			args:    nil,
			target:  &testStruct{},
			wantErr: false,
			verify: func(t *testing.T, target interface{}) {
				ts := target.(*testStruct)
				if ts.Name != "" || ts.Value != 0 {
					t.Errorf("expected zero values, got %+v", ts)
				}
			},
		},
		{
			name: "extra fields are ignored",
			args: map[string]interface{}{
				"name":  "test",
				"extra": "ignored",
			},
			target:  &testStruct{},
			wantErr: false,
		},
		{
			name: "marshal error",
			args: map[string]interface{}{
				"invalid": make(chan int),
			},
			target:  &testStruct{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UnmarshalArgs(tt.args, tt.target)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.verify != nil {
				tt.verify(t, tt.target)
			}
		})
	}
}

func TestToolResult_Structure(t *testing.T) {
	t.Parallel()
	res := ToolResult{
		Text: "output",
		BinaryData: []BinaryData{
			{MIMEType: "text/plain", Data: []byte("data")},
		},
	}

	if res.Text != "output" || len(res.BinaryData) != 1 {
		t.Error("structure mismatch")
	}
}

func TestErrNotImplemented(t *testing.T) {
	t.Parallel()
	if ErrNotImplemented.Error() != "not implemented" {
		t.Error("unexpected error message")
	}
}
