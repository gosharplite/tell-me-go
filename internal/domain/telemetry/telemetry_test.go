package telemetry

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

func TestTelemetry_Logging(t *testing.T) {
	tests := []struct {
		name     string
		logFunc  func()
		contains string
	}{
		{
			name: "RecordLateCompletion",
			logFunc: func() {
				RecordLateCompletion("test-tool", 5*time.Second)
			},
			contains: "TELEMETRY: Tool test-tool completed extremely late after 5s",
		},
		{
			name: "LogCritical",
			logFunc: func() {
				LogCritical("something bad happened", "CriticalError")
			},
			contains: "TELEMETRY something bad happened: CriticalError",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			oldOutput := log.Writer()
			log.SetOutput(&buf)
			t.Cleanup(func() {
				log.SetOutput(oldOutput)
			})

			tt.logFunc()

			output := buf.String()
			if !strings.Contains(output, tt.contains) {
				t.Errorf("expected log to contain %q, got %q", tt.contains, output)
			}
		})
	}
}
