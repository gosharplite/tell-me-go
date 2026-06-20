// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"runtime"
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
		runtime.Gosched() // cooperative yield: prevents fuzz shutdown race at 40s boundary (Issue #958)
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

// verifyPanickingSubscriber asserts that a panicking subscriber produces
// an error containing "subscriber panicked" and logs an ERROR entry.
func verifyPanickingSubscriber(t *testing.T, bus *SimpleEventBus, event Event, panicValue string, logBuf *bytes.Buffer) {
	t.Helper()

	panicSub := &panickingSubscriber{v: panicValue}
	err := bus.notifySubscriber(context.Background(), panicSub, event)

	if err == nil {
		t.Error("panicking subscriber: expected error, got nil")
	} else if !strings.Contains(err.Error(), "subscriber panicked") {
		t.Errorf("panicking subscriber: error %q does not contain 'subscriber panicked'", err.Error())
	}
	if !strings.Contains(logBuf.String(), `"level":"ERROR"`) {
		t.Error("panicking subscriber: expected ERROR log")
	}
	if !strings.Contains(logBuf.String(), "Subscriber panicked") {
		t.Error("panicking subscriber: expected 'Subscriber panicked' in log")
	}
}

// verifyErrorSubscriber asserts that an error-returning subscriber
// correctly propagates or suppresses errors based on errMsg.
func verifyErrorSubscriber(t *testing.T, bus *SimpleEventBus, event Event, errMsg string, logBuf *bytes.Buffer) {
	t.Helper()

	errSub := &funcSubscriberWithErr{f: func(ctx context.Context, e Event) error {
		if errMsg == "" {
			return nil
		}
		return errors.New(errMsg)
	}}
	err := bus.notifySubscriber(context.Background(), errSub, event)

	if errMsg == "" {
		if err != nil {
			t.Errorf("error subscriber (nil case): expected nil, got %v", err)
		}
	} else {
		if err == nil {
			t.Error("error subscriber: expected error, got nil")
		} else if !strings.Contains(err.Error(), errMsg) {
			t.Errorf("error subscriber: error %q does not contain %q", err.Error(), errMsg)
		}
	}
}

// verifyEventPassthrough asserts that an event reaches the subscriber
// intact with the correct Type() and StatusUpdate fields.
func verifyEventPassthrough(t *testing.T, bus *SimpleEventBus, event Event, eventMessage string, logBuf *bytes.Buffer) {
	t.Helper()

	var capturedEvent Event
	captureSub := &funcSubscriberWithErr{f: func(ctx context.Context, e Event) error {
		capturedEvent = e
		return nil
	}}
	_ = bus.notifySubscriber(context.Background(), captureSub, event)

	if capturedEvent == nil {
		t.Error("capture subscriber: event was nil")
	} else if capturedEvent.Type() != "StatusUpdate" {
		t.Errorf("capture subscriber: expected Type()='StatusUpdate', got %q", capturedEvent.Type())
	}
	if su, ok := capturedEvent.(StatusUpdate); ok {
		if su.Message != eventMessage {
			t.Errorf("capture subscriber: Message=%q, want %q", su.Message, eventMessage)
		}
	} else {
		t.Errorf("capture subscriber: expected StatusUpdate, got %T", capturedEvent)
	}
}

func FuzzNotifySubscriber(f *testing.F) {
	// Seed corpus: diverse panic values, error messages, and event messages.
	f.Add("boom", "sub err", "fuzz")                                     // baseline
	f.Add("", "", "")                                                    // all-empty edge case
	f.Add("\x00\nil", "oops\nil", "evt\nil")                             // embedded nils/newlines
	f.Add("\n", "\n", "\n")                                              // newline-only
	f.Add("", "disk full", "status: ok")                                 // empty panic, rich error
	f.Add("goroutine panic: send on closed channel", "timeout", "retry") // realistic

	f.Fuzz(func(t *testing.T, panicValue string, errMsg string, eventMessage string) {
		runtime.Gosched() // cooperative yield: prevents fuzz shutdown race at 40s boundary (Issue #958)
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
		bus := NewSimpleEventBus(context.Background(), WithLogger(logger))

		event := StatusUpdate{Message: eventMessage}

		verifyPanickingSubscriber(t, bus, event, panicValue, &logBuf)
		logBuf.Reset()
		verifyErrorSubscriber(t, bus, event, errMsg, &logBuf)
		logBuf.Reset()
		verifyEventPassthrough(t, bus, event, eventMessage, &logBuf)
	})
}
