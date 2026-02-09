// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"testing"
)

type testStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func verifyUnmarshal(t *testing.T, got, want testStruct) {
	t.Helper()
	if got.Name != want.Name || got.Value != want.Value {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestUnmarshalArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    map[string]interface{}
		want    testStruct
		wantErr bool
	}{
		{
			name: "valid conversion",
			args: map[string]interface{}{
				"name":  "test",
				"value": 123,
			},
			want:    testStruct{Name: "test", Value: 123},
			wantErr: false,
		},
		{
			name: "partial values",
			args: map[string]interface{}{
				"name": "test",
			},
			want:    testStruct{Name: "test", Value: 0},
			wantErr: false,
		},
		{
			name: "type mismatch",
			args: map[string]interface{}{
				"value": "not-an-int",
			},
			wantErr: true,
		},
		{
			name:    "nil map",
			args:    nil,
			want:    testStruct{Name: "", Value: 0},
			wantErr: false,
		},
		{
			name: "extra fields are ignored",
			args: map[string]interface{}{
				"name":  "test",
				"extra": "ignored",
			},
			want:    testStruct{Name: "test", Value: 0},
			wantErr: false,
		},
		{
			name: "marshal error",
			args: map[string]interface{}{
				"invalid": make(chan int),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var target testStruct
			err := UnmarshalArgs(tt.args, &target)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				verifyUnmarshal(t, target, tt.want)
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
