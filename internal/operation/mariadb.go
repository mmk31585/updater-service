package operation

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type MariaDBRepository struct {
	db *sql.DB
}

func NewMariaDBRepository(db *sql.DB) *MariaDBRepository {
	return &MariaDBRepository{
		db: db,
	}
}

func (r *MariaDBRepository) Create(
	ctx context.Context,
	op Operation,
) error {
	const query = `
		INSERT INTO operations (
			id,
			status,
			service,
			node_id,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.ExecContext(
		ctx,
		query,
		op.ID,
		op.Status,
		op.Service,
		op.NodeID,
		op.CreatedAt,
		op.UpdatedAt,
	)

	return err
}

func (r *MariaDBRepository) Get(
	ctx context.Context,
	id string,
) (Operation, error) {
	const query = `
		SELECT
			id,
			status,
			service,
			node_id,
			created_at,
			updated_at
		FROM operations
		WHERE id = ?
	`

	var op Operation

	err := r.db.QueryRowContext(
		ctx,
		query,
		id,
	).Scan(
		&op.ID,
		&op.Status,
		&op.Service,
		&op.NodeID,
		&op.CreatedAt,
		&op.UpdatedAt,
	)

	if err != nil {
		return Operation{}, err
	}

	return op, nil
}
func (r *MariaDBRepository) AdvanceStatus(
	ctx context.Context,
	id string,
	next Status,
) (bool, error) {
	rank, ok := statusRank(next)
	if !ok {
		return false, fmt.Errorf(
			"invalid operation status: %q",
			next,
		)
	}

	const query = `
		UPDATE operations
		SET
			status = ?,
			updated_at = ?
		WHERE
			id = ?
			AND (
				CASE status
					WHEN 'PENDING' THEN 0
					WHEN 'DISPATCHED' THEN 1
					WHEN 'RUNNING' THEN 2
					WHEN 'SUCCEEDED' THEN 3
					WHEN 'FAILED' THEN 3
					ELSE -1
				END < ?
				OR status = ?
			)
	`

	now := time.Now().UTC()

	result, err := r.db.ExecContext(
		ctx,
		query,
		next,
		now,
		id,
		rank,
		next,
	)
	if err != nil {
		return false, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}

	return rows > 0, nil
}

func statusRank(status Status) (int, bool) {
	switch status {
	case StatusPending:
		return 0, true
	case StatusDispatched:
		return 1, true
	case StatusRunning:
		return 2, true
	case StatusSucceeded:
		return 3, true
	case StatusFailed:
		return 3, true

	default:
		return 0, false
	}
}
