-- +goose Up
CREATE TABLE IF NOT EXISTS operations (
    id         VARCHAR(64)  NOT NULL,
    status     VARCHAR(32)  NOT NULL,
    service    VARCHAR(255) NOT NULL,
    node_id    VARCHAR(100) NOT NULL,
    created_at DATETIME(6)  NOT NULL,
    updated_at DATETIME(6)  NOT NULL,

    PRIMARY KEY (id),

    INDEX idx_operations_status_created_at (status, created_at),
    INDEX idx_operations_node_id (node_id)
) ENGINE=InnoDB
  DEFAULT CHARSET=utf8mb4
  COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS operations;
