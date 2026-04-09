package ioutils

import (
	"context"
	"io"
)

// contextReader is a wrapper for an io.Reader that respects a context.Context.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

// Read implements the io.Reader interface.
func (c *contextReader) Read(p []byte) (n int, err error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// NewContextReader returns a new io.Reader that wraps the given reader and honors the provided context.
func NewContextReader(ctx context.Context, r io.Reader) io.Reader {
	return &contextReader{
		ctx: ctx,
		r:   r,
	}
}
