package atlassian

import (
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// Replace 429 retry backoff with zero delay.
	// AtlassianProvider.Do still retries the same number of times
	// (4 attempts), but without sleeping — tests that mock 429
	// responses don't pay a 7-second penalty.
	defaultBaseDelay = 0 * time.Second

	os.Exit(m.Run())
}
