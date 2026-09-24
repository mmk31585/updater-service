package message

type UpdateCommand struct {
	OperationID  string `json:"operation_id"`
	Service      string `json:"service"`
	NodeInstance string `json:"node_instance"`

	FileURL    string `json:"file_url"`
	FileName   string `json:"file_name"`
	FileSize   int64  `json:"file_size"`
	FileSHA256 string `json:"file_sha256"`
}

type UpdateResult struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}
