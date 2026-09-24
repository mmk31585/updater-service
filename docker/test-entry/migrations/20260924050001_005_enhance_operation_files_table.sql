-- +goose Up
ALTER TABLE operation_files
ADD COLUMN status ENUM('UPLOADING', 'READY', 'TRANSFERRING', 'TRANSFERRED') 
NOT NULL DEFAULT 'UPLOADING';

-- +goose Down
ALTER TABLE operation_files
DROP COLUMN status;