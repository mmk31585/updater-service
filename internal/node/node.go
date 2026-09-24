package node

import "time"

type Status string

const (
	StatusOnline   Status = "ONLINE"
	StatusDraining Status = "DRAINING"
	StatusOffline  Status = "OFFLINE"
)

type Node struct {
	ID             string    `json:"id"`
	InstanceID     string    `json:"instance_id"`
	Status         Status    `json:"status"`
	Address        string    `json:"address"`
	Version        string    `json:"version"`
	Capabilities   []string  `json:"capabilities"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	StartedAt      time.Time `json:"started_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
