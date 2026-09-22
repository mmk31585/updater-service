package message

type UpdateCommand struct {
	OperationID string `json:"operation_id"`
	Service     string `json:"service"`
}
type OperationStatus string

const (
	StatusPending    OperationStatus = "PENDING"
	StatusDispatched OperationStatus = "DISPATCHED"
	StatusRunning    OperationStatus = "RUNNING"
	StatusSucceeded  OperationStatus = "SUCCEEDED"
)
type OperationEvent struct {
	OperationID string          `json:"operation_id"`
	Status      OperationStatus `json:"status"`
}