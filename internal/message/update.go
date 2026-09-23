package message

type UpdateCommand struct {
	OperationID string `json:"operation_id"`
	Service     string `json:"service"`
}

type UpdateResult struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
}