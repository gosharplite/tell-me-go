package telemetry

import (
	"log"
	"time"
)

// RecordLateCompletion logs a late completion.
func RecordLateCompletion(name string, d time.Duration) {
	log.Printf("TELEMETRY: Tool %s completed extremely late after %v", name, d)
}

// logCritical logs a critical event.
func logCritical(msg string, name string) {
	log.Printf("TELEMETRY %s: %s", msg, name)
}
