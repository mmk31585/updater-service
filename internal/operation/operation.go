package operation

import "time"

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusDispatched Status = "DISPATCHED"
	StatusRunning    Status = "RUNNING"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusFailed Status = "FAILED"
)

type Operation struct {
	ID        string `json:"id"`
	Status    Status `json:"status"`
	Service   string
	NodeID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}
