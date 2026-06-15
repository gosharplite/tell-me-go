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

		var sub Subscriber
		switch kind % 6 {
		case 0:
			sub = &funcSubscriberWithErr{f: func(ctx context.Context, e Event) error { return nil }}
		case 1:
			sub = &funcSubscriberWithErr{f: func(ctx context.Context, e Event) error { return errors.New("sub err") }}
		case 2:
			sub = &panickingSubscriber{v: "boom"}
		case 3:
			sub = &panickingSubscriber{v: nil}
		case 4:
			sub = &panickingSubscriber{v: errors.New("typed")}
		case 5:
			sub = &panickingSubscriber{v: ""}
		}

		event := StatusUpdate{Message: "fuzz"}
		ctx := context.Background()
		err := bus.notifySubscriber(ctx, sub, event)

		// Invariant: notifySubscriber always returns (never panics).
		// This is implicitly verified by the fuzzer reaching this point.

		switch {
		case kind%6 == 0:
			// Subscriber returns nil → err must be nil.
			if err != nil {
				t.Errorf("kind=0: expected nil error, got %v", err)
			}
		case kind%6 == 1:
			// Subscriber returns error → err must contain "sub err".
			if err == nil {
				t.Error("kind=1: expected error, got nil")
			} else if !strings.Contains(err.Error(), "sub err") {
				t.Errorf("kind=1: error %q does not contain %q", err.Error(), "sub err")
			}
		case kind%6 >= 2:
			// Subscriber panics → err must contain "subscriber panicked".
			if err == nil {
				t.Error("kind>=2: expected error from panic recovery, got nil")
			} else if !strings.Contains(err.Error(), "subscriber panicked") {
				t.Errorf("kind>=2: error %q does not contain %q", err.Error(), "subscriber panicked")
			}

			// Additionally verify structured log output.
			output := logBuf.String()
			if !strings.Contains(output, `"level":"ERROR"`) {
				t.Error("expected ERROR log on subscriber panic")
			}
			if !strings.Contains(output, "Subscriber panicked") {
				t.Error("expected 'Subscriber panicked' in log")
			}
		}
	})
}
