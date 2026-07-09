package lib

import "errors"

// ErrAlwaysFails is a sentinel error returned by FailingRunner.
var ErrAlwaysFails = errors.New("always fails")

// Runner is an interface with a single method.
type Runner interface {
	Run() error
}

// SimpleRunner implements Runner and always succeeds.
type SimpleRunner struct{}

// Run implements Runner.
func (s SimpleRunner) Run() error {
	return nil
}

// FailingRunner implements Runner and always fails.
type FailingRunner struct{}

// Run implements Runner.
func (f FailingRunner) Run() error {
	return ErrAlwaysFails
}
