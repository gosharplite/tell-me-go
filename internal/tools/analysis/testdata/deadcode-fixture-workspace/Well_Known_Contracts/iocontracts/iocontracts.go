package iocontracts
// Buffer satisfies io.Reader, io.Writer and io.Closer structurally, even
// though no call site in this test workspace ever names Read/Write/Close.
type Buffer struct{}
func (b *Buffer) Read(p []byte) (int, error)  { return 0, nil }
func (b *Buffer) Write(p []byte) (int, error) { return len(p), nil }
func (b *Buffer) Close() error                 { return nil }
