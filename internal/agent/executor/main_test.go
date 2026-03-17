package executor

import (
	"testing"
	"time"

	"go.uber.org/goleak"
)

const ciSafeTimeout = 3 * time.Second

func TestMain(m *testing.M) {
	// VerifyTestMain will fail the suite if any goroutine is leaked
	goleak.VerifyTestMain(m)
}
