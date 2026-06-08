// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"errors"
	"sync"
	"testing"
)

func TestMockEntropySource_Read_CopiesBytes(t *testing.T) {
	t.Parallel()

	wantData := []byte{0x01, 0x02}
	m := &MockEntropySource{
		ReadFunc: func(p []byte) (n int, err error) {
			copy(p, wantData)
			return len(wantData), nil
		},
	}

	buf := make([]byte, 8)
	n, err := m.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("got n %d; want 2", n)
	}
	if buf[0] != 0x01 || buf[1] != 0x02 {
		t.Errorf("got buf[0]=%d buf[1]=%d; want 1, 2", buf[0], buf[1])
	}

	calls, methods := m.Snapshot()
	if calls != 1 {
		t.Errorf("Read calls: got %d, want 1", calls)
	}
	if len(methods) != 1 || methods[0] != "Read" {
		t.Errorf("methods: got %v, want [Read]", methods)
	}
}

func TestMockEntropySource_Read_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("entropy exhausted")
	m := &MockEntropySource{
		ReadFunc: func(p []byte) (n int, err error) {
			return 0, wantErr
		},
	}

	buf := make([]byte, 4)
	n, err := m.Read(buf)
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if n != 0 {
		t.Errorf("got n %d; want 0", n)
	}

	calls, _ := m.Snapshot()
	if calls != 1 {
		t.Errorf("Read calls: got %d, want 1", calls)
	}
}

func TestMockEntropySource_Read_NilFunc(t *testing.T) {
	t.Parallel()

	m := &MockEntropySource{} // ReadFunc is nil

	buf := make([]byte, 8)
	n, err := m.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("got n %d; want 0", n)
	}

	calls, methods := m.Snapshot()
	if calls != 1 {
		t.Errorf("Read calls: got %d, want 1", calls)
	}
	if len(methods) != 1 || methods[0] != "Read" {
		t.Errorf("methods: got %v, want [Read]", methods)
	}
}

func TestMockEntropySource_RaceDetection(t *testing.T) {
	m := &MockEntropySource{}

	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 20

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			buf := make([]byte, 4)
			for i := 0; i < iterations; i++ {
				_, _ = m.Read(buf)
			}
		}()
	}
	wg.Wait()

	calls, _ := m.Snapshot()
	if calls != goroutines*iterations {
		t.Errorf("Read calls: got %d, want %d", calls, goroutines*iterations)
	}
}

func TestMockEntropySource_Snapshot_DefensiveCopy(t *testing.T) {
	t.Parallel()

	m := &MockEntropySource{}
	buf := make([]byte, 1)
	_, _ = m.Read(buf)
	_, _ = m.Read(buf)

	_, methods := m.Snapshot()
	// Mutate the returned slice — the internal state must not change
	methods[0] = "corrupted"

	_, methods2 := m.Snapshot()
	if methods2[0] != "Read" {
		t.Errorf("Snapshot must return a defensive copy; got %v", methods2)
	}
}
