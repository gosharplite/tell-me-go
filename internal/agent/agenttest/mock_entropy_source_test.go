// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"errors"
	"testing"
)

func TestMockEntropySource_Read_CopiesBytes(t *testing.T) {
	t.Parallel()

	src := new(MockEntropySource)
	buf := make([]byte, 8)
	wantData := []byte{0x01, 0x02}
	wantN := 2

	src.On("Read", buf).Return(wantData, wantN, nil)

	n, err := src.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != wantN {
		t.Errorf("got n %d; want %d", n, wantN)
	}
	if buf[0] != 0x01 || buf[1] != 0x02 {
		t.Errorf("got buf[0]=%d buf[1]=%d; want 1, 2", buf[0], buf[1])
	}
	src.AssertExpectations(t)
}

func TestMockEntropySource_Read_Error(t *testing.T) {
	t.Parallel()

	src := new(MockEntropySource)
	buf := make([]byte, 4)
	wantErr := errors.New("entropy exhausted")

	src.On("Read", buf).Return([]byte{}, 0, wantErr)

	n, err := src.Read(buf)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if n != 0 {
		t.Errorf("got n %d; want 0", n)
	}
	src.AssertExpectations(t)
}
