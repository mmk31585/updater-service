package operation

import "time"

type Status string

const (
	StatusPending                Status = "PENDING"
	StatusDispatched             Status = "DISPATCHED"
	StatusTransferred            Status = "TRANSFERRED"
	StatusRunning                Status = "RUNNING"
	StatusBackupCreated          Status = "BACKUP_CREATED"
	StatusApplying               Status = "APPLYING"
	StatusHealthChecking         Status = "HEALTH_CHECKING"
	StatusSucceeded              Status = "SUCCEEDED"
	StatusFailed                 Status = "FAILED"
	StatusRollingBack            Status = "ROLLING_BACK"
	StatusRollbackHealthChecking Status = "ROLLBACK_HEALTH_CHECKING"
	StatusRolledBack             Status = "ROLLED_BACK"
	StatusRollbackFailed         Status = "ROLLBACK_FAILED"
)

func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusRolledBack, StatusRollbackFailed:
		return true
	}
	return false
}

type Operation struct {
	ID        string `json:"id"`
	Status    Status `json:"status"`
	Service   string
	NodeID    string
	CreatedAt time.Time
	UpdatedAt time.Time
}
