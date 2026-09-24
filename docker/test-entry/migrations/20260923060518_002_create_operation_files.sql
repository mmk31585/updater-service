-- +goose Up
CREATE TABLE IF NOT EXISTS operation_files (
    operation_id VARCHAR(64) NOT NULL,
    file_name    VARCHAR(255) NOT NULL,
    file_path    TEXT NOT NULL,
    file_size    BIGINT UNSIGNED NOT NULL,
    sha256       CHAR(64) NOT NULL,
    created_at   DATETIME(6) NOT NULL,

    PRIMARY KEY (operation_id),

    CONSTRAINT fk_operation_files_operation
        FOREIGN KEY (operation_id)
        REFERENCES operations(id)
        ON DELETE CASCADE
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS operation_files;
