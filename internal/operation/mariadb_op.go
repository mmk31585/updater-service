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
					WHEN 'TRANSFERRED' THEN 3
					WHEN 'BACKUP_CREATED' THEN 4
					WHEN 'APPLYING' THEN 5
					WHEN 'HEALTH_CHECKING' THEN 6
					WHEN 'SUCCEEDED' THEN 7
					WHEN 'FAILED' THEN 7
					WHEN 'ROLLING_BACK' THEN 8
					WHEN 'ROLLBACK_HEALTH_CHECKING' THEN 9
					WHEN 'ROLLED_BACK' THEN 10
					WHEN 'ROLLBACK_FAILED' THEN 11
					ELSE -1
				END < ?
				OR (
					CASE status
						WHEN 'SUCCEEDED' THEN 7
						WHEN 'FAILED' THEN 7
						ELSE -1
					END = ?
					AND status = ?
				)
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
		7,
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

	case StatusTransferred:
		return 3, true

	case StatusBackupCreated:
		return 4, true

	case StatusApplying:
		return 5, true

	case StatusHealthChecking:
		return 6, true

	case StatusSucceeded:
		return 7, true

	case StatusFailed:
		return 7, true

	case StatusRollingBack:
		return 8, true

	case StatusRollbackHealthChecking:
		return 9, true

	case StatusRolledBack:
		return 10, true

	case StatusRollbackFailed:
		return 11, true

	default:
		return 0, false
	}
}
func (r *MariaDBRepository) ClaimForExecution(
	ctx context.Context,
	id string,
) (bool, Operation, error) {
	now := time.Now().UTC()

	const query = `
		UPDATE operations
		SET
			status = ?,
			updated_at = ?
		WHERE
			id = ?
			AND status = ?
	`

	result, err := r.db.ExecContext(
		ctx,
		query,
		StatusRunning,
		now,
		id,
		StatusDispatched,
	)
	if err != nil {
		return false, Operation{}, err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, Operation{}, err
	}

	op, err := r.Get(ctx, id)
	if err != nil {
		return false, Operation{}, err
	}

	return rows == 1, op, nil
}

func (r *MariaDBRepository) ListAll(
	ctx context.Context,
) ([]Operation, error) {
	const query = `
		SELECT
			id,
			status,
			service,
			node_id,
			created_at,
			updated_at
		FROM operations
		ORDER BY created_at ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	operations := make([]Operation, 0)

	for rows.Next() {
		var op Operation
		if err := rows.Scan(
			&op.ID,
			&op.Status,
			&op.Service,
			&op.NodeID,
			&op.CreatedAt,
			&op.UpdatedAt,
		); err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return operations, nil
}
