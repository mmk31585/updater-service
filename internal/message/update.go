package message

type UpdateCommand struct {
	OperationID string `json:"operation_id"`
	Service     string `json:"service"`
}
