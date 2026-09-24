package node

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("node not found")
	ErrNodeOffline   = errors.New("node offline")
	ErrStaleInstance = errors.New("stale node instance")
)

type Repository interface {
	Register(
		ctx context.Context,
		n Node,
	) error

	Heartbeat(
		ctx context.Context,
		nodeID string,
		instanceID string,
		leaseExpiresAt time.Time,
	) error

	Goodbye(
		ctx context.Context,
		nodeID string,
		instanceID string,
	) error

	MarkExpired(
		ctx context.Context,
		now time.Time,
	) error

	Get(
		ctx context.Context,
		id string,
	) (Node, error)

	List(
		ctx context.Context,
	) ([]Node, error)

	SetDraining(
		ctx context.Context,
		id string,
		draining bool,
	) error
}
type MariaDBNodeRepository struct {
	db *sql.DB
}

func NewMariaDBNodeRepository(db *sql.DB) *MariaDBNodeRepository {
	return &MariaDBNodeRepository{
		db: db,
	}
}
func (r *MariaDBNodeRepository) Register(
	ctx context.Context,
	n Node,
) error {
	capabilities, err := json.Marshal(
		n.Capabilities,
	)
	if err != nil {
		return err
	}

	const query = `
		INSERT INTO nodes (
			id,
			instance_id,
			status,
			address,
			version,
			capabilities,
			last_seen_at,
			lease_expires_at,
			started_at,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)

		ON DUPLICATE KEY UPDATE
			instance_id = VALUES(instance_id),
			status = CASE
				WHEN status = 'DRAINING' THEN 'DRAINING'
				ELSE 'ONLINE'
			END,
			address = VALUES(address),
			version = VALUES(version),
			capabilities = VALUES(capabilities),
			last_seen_at = VALUES(last_seen_at),
			lease_expires_at = VALUES(lease_expires_at),
			started_at = VALUES(started_at),
			updated_at = VALUES(updated_at)
	`

	_, err = r.db.ExecContext(
		ctx,
		query,
		n.ID,
		n.InstanceID,
		StatusOnline,
		n.Address,
		n.Version,
		capabilities,
		n.LastSeenAt,
		n.LeaseExpiresAt,
		n.StartedAt,
		n.CreatedAt,
		n.UpdatedAt,
	)

	return err
}
func (r *MariaDBNodeRepository) Heartbeat(
	ctx context.Context,
	nodeID string,
	instanceID string,
	leaseExpiresAt time.Time,
) error {
	const query = `
		UPDATE nodes
		SET
			status = CASE
				WHEN status = 'DRAINING'
					THEN 'DRAINING'
				ELSE 'ONLINE'
			END,
			last_seen_at = ?,
			lease_expires_at = ?,
			updated_at = ?
		WHERE
			id = ?
			AND instance_id = ?
	`

	now := time.Now().UTC()

	result, err := r.db.ExecContext(
		ctx,
		query,
		now,
		leaseExpiresAt,
		now,
		nodeID,
		instanceID,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrStaleInstance
	}
	return nil
}
func (r *MariaDBNodeRepository) Goodbye(
	ctx context.Context,
	nodeID string,
	instanceID string,
) error {
	const query = `
		UPDATE nodes
		SET
			status = CASE
				WHEN status = 'DRAINING' THEN 'DRAINING'
				ELSE 'OFFLINE'
			END,
			lease_expires_at = ?,
			updated_at = ?
		WHERE
			id = ?
			AND instance_id = ?
	`

	now := time.Now().UTC()

	result, err := r.db.ExecContext(
		ctx,
		query,
		now,
		now,
		nodeID,
		instanceID,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrStaleInstance
	}
	return nil
}
func (r *MariaDBNodeRepository) MarkExpired(
	ctx context.Context,
	now time.Time,
) error {
	const query = `
		UPDATE nodes
		SET
			status = CASE
				WHEN status = 'DRAINING' THEN 'DRAINING'
				ELSE 'OFFLINE'
			END,
			updated_at = ?
		WHERE
			status IN ('ONLINE', 'DRAINING')
			AND lease_expires_at <= ?
	`

	_, err := r.db.ExecContext(
		ctx,
		query,
		now,
		now,
	)

	return err
}
func (r *MariaDBNodeRepository) SetDraining(
	ctx context.Context,
	id string,
	draining bool,
) error {
	status := StatusOnline

	if draining {
		status = StatusDraining
	}

	const query = `
		UPDATE nodes
		SET
			status = ?,
			updated_at = ?
		WHERE id = ?
			AND status != 'OFFLINE'
	`

	result, err := r.db.ExecContext(
		ctx,
		query,
		status,
		time.Now().UTC(),
		id,
	)
	if err != nil {
		return err
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}

	record, err := r.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if record.Status == StatusOffline {
		return ErrNodeOffline
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(scanner rowScanner) (Node, error) {
	var (
		record       Node
		status       string
		capabilities sql.NullString
	)

	err := scanner.Scan(
		&record.ID,
		&record.InstanceID,
		&status,
		&record.Address,
		&record.Version,
		&capabilities,
		&record.LastSeenAt,
		&record.LeaseExpiresAt,
		&record.StartedAt,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	if err != nil {
		return Node{}, err
	}

	record.Status = Status(status)
	if capabilities.Valid && capabilities.String != "" {
		if err := json.Unmarshal([]byte(capabilities.String), &record.Capabilities); err != nil {
			return Node{}, err
		}
	}
	return record, nil
}

func (r *MariaDBNodeRepository) Get(
	ctx context.Context,
	id string,
) (Node, error) {
	const query = `
		SELECT
			id,
			instance_id,
			status,
			address,
			version,
			capabilities,
			last_seen_at,
			lease_expires_at,
			started_at,
			created_at,
			updated_at
		FROM nodes
		WHERE id = ?
	`

	record, err := scanNode(r.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Node{}, ErrNotFound
	}
	return record, err
}

func (r *MariaDBNodeRepository) List(ctx context.Context) ([]Node, error) {
	const query = `
		SELECT
			id,
			instance_id,
			status,
			address,
			version,
			capabilities,
			last_seen_at,
			lease_expires_at,
			started_at,
			created_at,
			updated_at
		FROM nodes
		ORDER BY status, id
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]Node, 0)
	for rows.Next() {
		record, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
