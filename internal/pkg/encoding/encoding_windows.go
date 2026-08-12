// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package encoding

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"syscall"
	"unicode/utf8"

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
	getACPProc             = kernel32.NewProc("GetACP")
)

func getConsoleOutputCP() uint32 {
	r0, _, _ := getConsoleOutputCPProc.Call()
	return uint32(r0)
}

func getACP() uint32 {
	r0, _, _ := getACPProc.Call()
	return uint32(r0)
}

func wrapReaderPlatform(r io.Reader) io.Reader {
	// Priority 1: Check environment for UTF-8 override.
	// We only trust this if the system is natively UTF-8.
	if isUTF8Env(os.Getenv) && getACP() == 65001 {
		return r
	}

	fallbackCP := getConsoleOutputCP()
	acp := getACP()

	if fallbackCP == 65001 && acp != 65001 {
		fallbackCP = acp
	}

	if fallbackCP == 65001 {
		return r
	}

	return &sniffingReader{r: r, fallbackCP: fallbackCP}
}

func getReaderForCP(r io.Reader, cp uint32) io.Reader {
	var enc encoding.Encoding
	switch cp {
	case 65001:
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
		slog.Debug("unknown windows console code page, falling back to raw reader", slog.Int("codepage", int(cp)))
		return r
	}

	if enc != nil {
		return enc.NewDecoder().Reader(r)
	}
	return r
}

type sniffingReader struct {
	r          io.Reader
	fallbackCP uint32
	decoded    io.Reader
	buf        bytes.Buffer
	err        error
}

func (s *sniffingReader) Read(p []byte) (int, error) {
	if s.decoded != nil {
		return s.decoded.Read(p)
	}
	if s.err != nil {
		return 0, s.err
	}

	if err := s.fillBuffer(); err != nil && err != io.EOF {
		s.err = err
		return 0, err
	}

	if s.buf.Len() > 0 {
		s.decoded = s.initializeDecoder()
		return s.decoded.Read(p)
	}

	return 0, io.EOF
}

func (s *sniffingReader) fillBuffer() error {
	for s.buf.Len() < 4096 {
		tmp := make([]byte, 4096-s.buf.Len())
		n, err := s.r.Read(tmp)
		if n > 0 {
			s.buf.Write(tmp[:n])
		}
		if err != nil {
			return err // Return EOF or other read errors
		}

		// Heuristic: Stop early if we have a definitive signal
		if s.isDefinitive(s.buf.Bytes()) {
			break
		}
	}
	return nil
}

func (s *sniffingReader) isDefinitive(data []byte) bool {
	// UTF-8 signal: Found multi-byte sequence
	if containsUTF8MultiByte(data) {
		return true
	}
	// Fallback signal: Found non-ASCII that is NOT valid UTF-8
	if containsNonASCII(data) && !utf8.Valid(data) {
		return true
	}
	return false
}

func (s *sniffingReader) initializeDecoder() io.Reader {
	fullReader := io.MultiReader(bytes.NewReader(s.buf.Bytes()), s.r)

	if utf8.Valid(s.buf.Bytes()) {
		return fullReader
	}
	return getReaderForCP(fullReader, s.fallbackCP)
}

func containsNonASCII(b []byte) bool {
	for _, c := range b {
		if c > 127 {
			return true
		}
	}
	return false
}

func containsUTF8MultiByte(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] > 127 {
			r, size := utf8.DecodeRune(b[i:])
			if r != utf8.RuneError && size > 1 {
				return true
			}
		}
	}
	return false
}
