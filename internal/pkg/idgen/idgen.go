// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"time"
)

// Generate creates a unique session identifier using crypto/rand.Reader:
// "session-" + 16 lowercase hex characters (8 bytes of entropy).
func Generate() string {
	return GenerateWithEntropy(rand.Reader)
}

// GenerateWithEntropy creates a session identifier using the provided entropy source.
// If reading 8 bytes from r fails, it falls back to a timestamp: "session-%d" (UnixNano).
func GenerateWithEntropy(r io.Reader) string {
	b := make([]byte, 8)
	if _, err := io.ReadFull(r, b); err != nil {
		return fmt.Sprintf("session-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("session-%s", hex.EncodeToString(b))
}
