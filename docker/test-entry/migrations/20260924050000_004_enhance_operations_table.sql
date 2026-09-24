-- +goose Up
ALTER TABLE operations
ADD COLUMN attempt INT NOT NULL DEFAULT 1,
ADD COLUMN started_at DATETIME(6) NULL,
ADD COLUMN finished_at DATETIME(6) NULL,
ADD COLUMN error_message TEXT NULL;

-- +goose Down
ALTER TABLE operations
DROP COLUMN attempt,
DROP COLUMN started_at,
DROP COLUMN finished_at,
DROP COLUMN error_message;