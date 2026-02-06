package executor

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// VerifyTestMain will fail the suite if any goroutine is leaked
	goleak.VerifyTestMain(m)
}
