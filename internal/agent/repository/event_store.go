package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// event represents an orchestration event.
type event struct {
	ID        string
	Payload   string
	CreatedAt time.Time
}

// eventStore handles database access for events.
type eventStore struct {
	db *sql.DB
}

// newEventStore creates a new eventStore.
func newEventStore(db *sql.DB) *eventStore {
	return &eventStore{db: db}
}

// getSessionEvents fetches multiple events in a single database round-trip
// to prevent N+1 query bottlenecks.
func (r *eventStore) getSessionEvents(ctx context.Context, eventIDs []string) (events []event, err error) {
	if len(eventIDs) == 0 {
		return nil, nil
	}

	query, args := buildSessionEventsQuery(eventIDs)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying session events: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("closing event rows: %w", closeErr)
			}
		}
	}()

	events, err = parseSessionEvents(rows)
	if err != nil {
		return nil, err
	}

	return events, nil
}

func buildSessionEventsQuery(eventIDs []string) (string, []interface{}) {
	placeholders := strings.Repeat("?,", len(eventIDs)-1) + "?"
	query := "SELECT id, payload, created_at FROM events WHERE id IN (" + placeholders + ")"

	args := make([]interface{}, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	return query, args
}

func parseSessionEvents(rows *sql.Rows) ([]event, error) {
	var events []event
	for rows.Next() {
		var e event
		var createdAtStr string
		if err := rows.Scan(&e.ID, &e.Payload, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}

		// Parse the string time returned by SQLite
		var parseErr error
		e.CreatedAt, parseErr = time.Parse(time.RFC3339Nano, createdAtStr)
		if parseErr != nil {
			return nil, fmt.Errorf("parsing created_at for event %s: %w", e.ID, parseErr)
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("event rows iteration: %w", err)
	}

	return events, nil
}
