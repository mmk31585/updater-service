package operation

import (
	"context"
	"database/sql"
	"time"
)

type TimeoutMonitor struct {
	db       *sql.DB
	timeout  time.Duration
	interval time.Duration
}

func NewTimeoutMonitor(
	db *sql.DB,
	timeout time.Duration,
	interval time.Duration,
) *TimeoutMonitor {
	return &TimeoutMonitor{
		db:       db,
		timeout:  timeout,
		interval: interval,
	}
}

func (m *TimeoutMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Run once immediately.
	m.check(ctx)

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			m.check(ctx)
		}
	}
}

func (m *TimeoutMonitor) check(
	ctx context.Context,
) {
	cutoff := time.Now().
		UTC().
		Add(-m.timeout)

	const query = `
		UPDATE operations
		SET
			status = ?,
			updated_at = ?
		WHERE
			status IN (?, ?, ?, ?)
			AND updated_at < ?
	`

	now := time.Now().UTC()

	_, _ = m.db.ExecContext(
		ctx,
		query,
		StatusFailed,
		now,
		StatusDispatched,
		StatusRunning,
		StatusTransferred,
		StatusHealthChecking,
		cutoff,
	)
}
