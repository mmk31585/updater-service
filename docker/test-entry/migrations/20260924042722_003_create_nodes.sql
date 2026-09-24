-- +goose Up
CREATE TABLE IF NOT EXISTS nodes (
    id              VARCHAR(64)  NOT NULL,
    instance_id     VARCHAR(128) NOT NULL,
    status          VARCHAR(16)  NOT NULL,
    address         VARCHAR(255) NOT NULL,
    version         VARCHAR(64)  NOT NULL,

    capabilities    JSON NULL,

    last_seen_at    DATETIME(6) NOT NULL,
    lease_expires_at DATETIME(6) NOT NULL,
    started_at      DATETIME(6) NOT NULL,

    created_at      DATETIME(6) NOT NULL,
    updated_at      DATETIME(6) NOT NULL,

    PRIMARY KEY (id),

    INDEX idx_nodes_status (status),
    INDEX idx_nodes_lease_expires_at (lease_expires_at)
) ENGINE=InnoDB
DEFAULT CHARSET=utf8mb4
COLLATE=utf8mb4_unicode_ci;
-- +goose Down
DROP TABLE IF EXISTS nodes;
