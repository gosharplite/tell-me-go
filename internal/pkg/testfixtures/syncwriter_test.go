// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testfixtures

import (
	"bytes"
	"testing"
)

// noStringWriter implements io.Writer but intentionally does NOT implement
// fmt.Stringer. Its Write method delegates to an internal bytes.Buffer so
// that we can verify SyncWriter.String falls back to the internal buffer
// when the external writer lacks a String() method.
type noStringWriter struct {
	buf bytes.Buffer
}

func (w *noStringWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

func TestSyncWriter_Write(t *testing.T) {
	tests := []struct {
		name   string
		sw     *SyncWriter
		data   []byte
		verify func(t *testing.T, sw *SyncWriter, n int, err error)
	}{
		{
			name: "writes to external writer",
			sw:   &SyncWriter{Writer: &bytes.Buffer{}},
			data: []byte("hello"),
			verify: func(t *testing.T, sw *SyncWriter, n int, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if n != 5 {
					t.Errorf("n = %d; want 5", n)
				}
				buf := sw.Writer.(*bytes.Buffer)
				if buf.String() != "hello" {
					t.Errorf("external buffer = %q; want %q", buf.String(), "hello")
				}
			},
		},
		{
			name: "writes to internal buffer when Writer is nil",
			sw:   &SyncWriter{},
			data: []byte("hello"),
			verify: func(t *testing.T, sw *SyncWriter, n int, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if n != 5 {
					t.Errorf("n = %d; want 5", n)
				}
				if sw.String() != "hello" {
					t.Errorf("String() = %q; want %q", sw.String(), "hello")
				}
			},
		},
		{
			name: "sends OnWrite notification",
			sw:   &SyncWriter{OnWrite: make(chan struct{}, 1)},
			data: []byte("x"),
			verify: func(t *testing.T, sw *SyncWriter, n int, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				select {
				case <-sw.OnWrite:
					// OK: notification received
				default:
					t.Error("expected OnWrite notification but none received")
				}
			},
		},
		{
			name: "OnWrite nil does not panic",
			sw:   &SyncWriter{OnWrite: nil},
			data: []byte("x"),
			verify: func(t *testing.T, sw *SyncWriter, n int, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if n != 1 {
					t.Errorf("n = %d; want 1", n)
				}
			},
		},
		{
			name: "OnWrite full does not block",
			sw:   &SyncWriter{OnWrite: make(chan struct{})},
			data: []byte("x"),
			verify: func(t *testing.T, sw *SyncWriter, n int, err error) {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if n != 1 {
					t.Errorf("n = %d; want 1", n)
				}
				// If we reached here, Write did not block — the default
				// case in select was taken. That is the expected behaviour.
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n, err := tt.sw.Write(tt.data)
			tt.verify(t, tt.sw, n, err)
		})
	}
}

func TestSyncWriter_String(t *testing.T) {
	tests := []struct {
		name  string
		setup func() *SyncWriter
		want  string
	}{
		{
			name: "delegates to external writer with String() method",
			setup: func() *SyncWriter {
				sw := &SyncWriter{Writer: &bytes.Buffer{}}
				_, _ = sw.Write([]byte("abc"))
				return sw
			},
			want: "abc",
		},
		{
			name: "returns internal buffer when Writer is nil",
			setup: func() *SyncWriter {
				sw := &SyncWriter{}
				_, _ = sw.Write([]byte("xyz"))
				return sw
			},
			want: "xyz",
		},
		{
			name: "returns internal buffer when Writer lacks String()",
			setup: func() *SyncWriter {
				sw := &SyncWriter{Writer: &noStringWriter{}}
				_, _ = sw.Write([]byte("discarded"))
				return sw
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sw := tt.setup()
			if got := sw.String(); got != tt.want {
				t.Errorf("String() = %q; want %q", got, tt.want)
			}
		})
	}
}
