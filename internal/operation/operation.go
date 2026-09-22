package operation

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusDispatched Status = "DISPATCHED"
	StatusRunning    Status = "RUNNING"
	StatusSucceeded  Status = "SUCCEEDED"
)

type Operation struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
}