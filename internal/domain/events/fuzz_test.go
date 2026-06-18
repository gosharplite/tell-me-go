// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"
)

func FuzzLimitsValidate(f *testing.F) {
	// Seed corpus per specification.
	f.Add(0, 0, 0)                               // All-zero defaults (valid)
	f.Add(8000, 10, 50)                          // All-positive typical config (valid)
	f.Add(-1, 0, 0)                              // Negative MaxHistoryTokens (invalid)
	f.Add(0, -1, 0)                              // Negative MaxToolTurns (invalid)
	f.Add(0, 0, -1)                              // Negative MaxHistoryTurns (invalid)
	f.Add(-1, -1, -1)                            // All-negative (invalid)
	f.Add(math.MinInt, math.MinInt, math.MinInt) // Extreme negative (invalid)

	f.Fuzz(func(t *testing.T, maxHistoryTokens, maxToolTurns, maxHistoryTurns int) {
		limits := Limits{
			MaxHistoryTokens: maxHistoryTokens,
			MaxToolTurns:     maxToolTurns,
			MaxHistoryTurns:  maxHistoryTurns,
		}
		err := limits.Validate()

		if err == nil {
			// Contract: if Validate passes, all three fields must be >= 0.
			if maxHistoryTokens < 0 {
				t.Errorf("Validate returned nil but MaxHistoryTokens is negative: %d", maxHistoryTokens)
			}
			if maxToolTurns < 0 {
				t.Errorf("Validate returned nil but MaxToolTurns is negative: %d", maxToolTurns)
			}
			if maxHistoryTurns < 0 {
				t.Errorf("Validate returned nil but MaxHistoryTurns is negative: %d", maxHistoryTurns)
			}
			return
		}

		// Contract: if Validate fails, the error message must reference one of
		// the three validated fields (case-insensitive).
		errMsg := err.Error()
		if !strings.Contains(errMsg, "max history tokens") &&
			!strings.Contains(errMsg, "max tool turns") &&
			!strings.Contains(errMsg, "max history turns") {
			t.Errorf("error message %q does not contain any expected field reference", errMsg)
		}
	})
}

// panickingSubscriber is used by FuzzNotifySubscriber to inject specific panic values.
type panickingSubscriber struct {
	v any
}

func (s *panickingSubscriber) Handle(ctx context.Context, e Event) error {
	panic(s.v)
}

// funcSubscriberWithErr is used by FuzzNotifySubscriber to inject specific error returns.
type funcSubscriberWithErr struct {
	f func(context.Context, Event) error
}

func (s *funcSubscriberWithErr) Handle(ctx context.Context, e Event) error {
	return s.f(ctx, e)
}

type notifyExpectation struct {
	sub          Subscriber
	wantErrNil   bool   // true if err should be nil
	wantErrCont  string // substring that error must contain (empty = skip check)
	wantPanicLog bool   // true if ERROR log expected
	desc         string // human-readable label
}

func FuzzNotifySubscriber(f *testing.F) {
	// Seed corpus per specification.
	f.Add(int64(0)) // Happy path: subscriber returns nil
	f.Add(int64(1)) // Error propagation: subscriber returns error
	f.Add(int64(2)) // Panic with string "boom": most common panic form
	f.Add(int64(3)) // Panic with nil: edge case panic(nil)
	f.Add(int64(4)) // Panic with error-typed value
	f.Add(int64(5)) // Panic with empty string: edge case

	f.Fuzz(func(t *testing.T, kind int64) {
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
		bus := NewSimpleEventBus(context.Background(), WithLogger(logger))

		expectations := map[int64]notifyExpectation{
			0: {sub: &funcSubscriberWithErr{f: func(ctx context.Context, e Event) error { return nil }}, wantErrNil: true, desc: "happy path"},
			1: {sub: &funcSubscriberWithErr{f: func(ctx context.Context, e Event) error { return errors.New("sub err") }}, wantErrCont: "sub err", desc: "error propagation"},
			2: {sub: &panickingSubscriber{v: "boom"}, wantErrCont: "subscriber panicked", wantPanicLog: true, desc: "panic with string"},
			3: {sub: &panickingSubscriber{v: nil}, wantErrCont: "subscriber panicked", wantPanicLog: true, desc: "panic with nil"},
			4: {sub: &panickingSubscriber{v: errors.New("typed")}, wantErrCont: "subscriber panicked", wantPanicLog: true, desc: "panic with error type"},
			5: {sub: &panickingSubscriber{v: ""}, wantErrCont: "subscriber panicked", wantPanicLog: true, desc: "panic with empty string"},
		}

		exp := expectations[kind%6]
		event := StatusUpdate{Message: "fuzz"}
		ctx := context.Background()
		err := bus.notifySubscriber(ctx, exp.sub, event)
		assertNotifyResult(t, exp, err, logBuf.String())
	})
}

func assertNotifyResult(t *testing.T, exp notifyExpectation, err error, logOutput string) {
	t.Helper()
	// Invariant: notifySubscriber always returns (never panics)
	if exp.wantErrNil {
		if err != nil {
			t.Errorf("%s: expected nil error, got %v", exp.desc, err)
		}
		return
	}
	if err == nil {
		t.Errorf("%s: expected error, got nil", exp.desc)
		return
	}
	if exp.wantErrCont != "" && !strings.Contains(err.Error(), exp.wantErrCont) {
		t.Errorf("%s: error %q does not contain %q", exp.desc, err.Error(), exp.wantErrCont)
	}
	if exp.wantPanicLog {
		if !strings.Contains(logOutput, `"level":"ERROR"`) {
			t.Error("expected ERROR log on subscriber panic")
		}
		if !strings.Contains(logOutput, "Subscriber panicked") {
			t.Error("expected 'Subscriber panicked' in log")
		}
	}
}
