// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package encoding

import (
	"io"
	"log/slog"
	"os"
	"syscall"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	getConsoleOutputCPProc = kernel32.NewProc("GetConsoleOutputCP")
)

func getConsoleOutputCP() uint32 {
	r0, _, _ := getConsoleOutputCPProc.Call()
	return uint32(r0)
}

func wrapReaderPlatform(r io.Reader) io.Reader {
	// Priority 1: Check environment for UTF-8 override
	if isUTF8Env(os.Getenv) {
		return r
	}

	// Priority 2: Fallback to system console code page
	cp := getConsoleOutputCP()
	var enc encoding.Encoding

	switch cp {
	case 65001: // UTF-8
		return r
	case 950:
		enc = traditionalchinese.Big5
	case 936:
		enc = simplifiedchinese.GBK
	case 932:
		enc = japanese.ShiftJIS
	case 949:
		enc = korean.EUCKR
	case 1252:
		enc = charmap.Windows1252
	default:
		// Fallback to no decoding if unknown
		slog.Debug("unknown windows console code page, falling back to raw reader", slog.Int("codepage", int(cp)))
		return r
	}

	if enc != nil {
		return enc.NewDecoder().Reader(r)
	}
	return r
}
