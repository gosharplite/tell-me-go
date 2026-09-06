// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package idgen_test

import (
	"bytes"
	"io"
	"regexp"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pkg/idgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingReader struct {
	err error
}

func (f *failingReader) Read(p []byte) (n int, err error) {
	return 0, f.err
}

func TestGenerate_Format(t *testing.T) {
	t.Parallel()
	pattern := regexp.MustCompile(`^session-[0-9a-f]{16}$`)
	for i := 0; i < 100; i++ {
		id := idgen.Generate()
		assert.Regexp(t, pattern, id)
	}
}

func TestGenerateWithEntropy_Deterministic(t *testing.T) {
	t.Parallel()
	entropy := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	reader := bytes.NewReader(entropy)
	id := idgen.GenerateWithEntropy(reader)
	assert.Equal(t, "session-0123456789abcdef", id)
}

func TestGenerateWithEntropy_Fallback(t *testing.T) {
	t.Parallel()
	reader := &failingReader{err: io.ErrUnexpectedEOF}
	id := idgen.GenerateWithEntropy(reader)
	pattern := regexp.MustCompile(`^session-\d+$`)
	assert.Regexp(t, pattern, id)
}

func TestGenerateWithEntropy_Fallback_Callback(t *testing.T) {
	t.Parallel()
	expectedErr := io.ErrUnexpectedEOF
	reader := &failingReader{err: expectedErr}
	var calledWith error
	id := idgen.GenerateWithEntropy(reader, func(err error) {
		calledWith = err
	})
	pattern := regexp.MustCompile(`^session-\d+$`)
	assert.Regexp(t, pattern, id)
	assert.Equal(t, expectedErr, calledWith)
}

func TestGenerate_Uniqueness(t *testing.T) {
	t.Parallel()
	const count = 1000
	seen := make(map[string]struct{}, count)
	for i := 0; i < count; i++ {
		id := idgen.Generate()
		require.NotContains(t, seen, id, "duplicate session ID generated at iteration %d", i)
		seen[id] = struct{}{}
	}
}
