package operation

import (
	"context"
	"database/sql"
)

type MariaDBFileRepository struct {
	db *sql.DB
}

func NewMariaDBFileRepository(db *sql.DB) *MariaDBFileRepository {
	return &MariaDBFileRepository{db: db}
}

func (r *MariaDBFileRepository) Create(ctx context.Context, meta FileMetadata) error {
	const query = `
		INSERT INTO operation_files (
			operation_id,
			file_name,
			file_path,
			file_size,
			sha256,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	_, err := r.db.ExecContext(
		ctx,
		query,
		meta.OperationID,
		meta.FileName,
		meta.FilePath,
		meta.FileSize,
		meta.SHA256,
		meta.CreatedAt,
	)
	return err
}

func (r *MariaDBFileRepository) Get(ctx context.Context, operationID string) (FileMetadata, error) {
	const query = `
		SELECT
			operation_id,
			file_name,
			file_path,
			file_size,
			sha256,
			created_at
		FROM operation_files
		WHERE operation_id = ?
	`
	var meta FileMetadata
	err := r.db.QueryRowContext(ctx, query, operationID).Scan(
		&meta.OperationID,
		&meta.FileName,
		&meta.FilePath,
		&meta.FileSize,
		&meta.SHA256,
		&meta.CreatedAt,
	)
	if err != nil {
		return FileMetadata{}, err
	}
	return meta, nil
}
