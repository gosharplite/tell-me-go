package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSessionEvents_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnError(errors.New("connection failed"))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "querying session events")
	assert.Contains(t, err.Error(), "connection failed")
	assert.Nil(t, events)
}

func TestGetSessionEvents_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := newEventStore(db)
	// Passing a NULL (nil) to a non-nullable Scan target (string) will trigger a scan error
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", "payload", nil))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scanning event row")
	assert.Nil(t, events)
}

func TestGetSessionEvents_ParseTimeError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := newEventStore(db)
	eventID := "event-bad-time"
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs(eventID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow(eventID, "payload", "invalid-date"))

	events, err := r.getSessionEvents(context.Background(), []string{eventID})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("parsing created_at for event %s", eventID))
	assert.Nil(t, events)
}

func TestGetSessionEvents_IterationError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", "payload", "2023-10-27T10:00:00Z").
			RowError(0, errors.New("iteration failure")))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "event rows iteration")
	assert.Contains(t, err.Error(), "iteration failure")
	assert.Nil(t, events)
}

func TestGetSessionEvents_CloseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", "payload", "2023-10-27T10:00:00Z").
			CloseError(errors.New("failed to close rows")))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	assert.Error(t, err)
	// We check for the specific error message. Depending on the mock/driver behavior,
	// it might be captured by rows.Err() or the explicit rows.Close() in defer.
	assert.Contains(t, err.Error(), "failed to close rows")
	assert.Nil(t, events)
}

// TestGetSessionEvents_CloseErrorPropagationPath proves that close errors
// ARE propagated to the caller — they just flow through the rows.Err() path
// inside parseSessionEvents rather than the err==nil defer branch.
//
// With database/sql semantics:
//  1. When rows.Next() exhausts the result set, the sql package calls
//     the driver's Close internally and surfaces any error via rows.Err().
//  2. parseSessionEvents calls rows.Err() after the scan loop and wraps it:
//     "event rows iteration: <close error>"
//  3. This makes err non-nil when getSessionEvents evaluates its defer.
//  4. The defer's err==nil guard is therefore unreachable in practice —
//     it exists as defensive code only (see inline comment in event_store.go).
//
// This test exercises the full propagation chain: data parses successfully,
// CloseError fires, the error reaches the caller.
func TestGetSessionEvents_CloseErrorPropagationPath(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	r := newEventStore(db)
	mock.ExpectQuery("SELECT id, payload, created_at FROM events").
		WithArgs("event-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload", "created_at"}).
			AddRow("event-1", `{"status": "ok"}`, "2023-10-27T10:00:00Z").
			CloseError(errors.New("close failed")))

	events, err := r.getSessionEvents(context.Background(), []string{"event-1"})

	// The close error is propagated — database/sql surfaces it through
	// rows.Err() inside parseSessionEvents, not through the defer's
	// rows.Close() (which runs second and finds err already non-nil).
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "close failed")
	assert.Nil(t, events)
}
