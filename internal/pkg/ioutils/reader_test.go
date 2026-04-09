package ioutils

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestContextReader_Read(t *testing.T) {
	t.Run("successful read", func(t *testing.T) {
		content := "hello world"
		r := NewContextReader(context.Background(), strings.NewReader(content))
		buf := make([]byte, len(content))
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != len(content) {
			t.Errorf("got %d bytes; want %d", n, len(content))
		}
		if string(buf) != content {
			t.Errorf("got %q; want %q", string(buf), content)
		}
	})

	t.Run("context cancelled before read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := NewContextReader(ctx, strings.NewReader("hello"))
		buf := make([]byte, 5)
		n, err := r.Read(buf)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("got error %v; want %v", err, context.Canceled)
		}
		if n != 0 {
			t.Errorf("got %d bytes; want 0", n)
		}
	})

	t.Run("underlying reader error", func(t *testing.T) {
		errMock := errors.New("mock error")
		r := NewContextReader(context.Background(), errorReader{err: errMock})
		buf := make([]byte, 5)
		n, err := r.Read(buf)
		if !errors.Is(err, errMock) {
			t.Errorf("got error %v; want %v", err, errMock)
		}
		if n != 0 {
			t.Errorf("got %d bytes; want 0", n)
		}
	})
}

type errorReader struct {
	err error
}

func (e errorReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}
