package operation

import "time"

type FileMetadata struct {
	OperationID string    `json:"operation_id" db:"operation_id"`
	FileName    string    `json:"file_name" db:"file_name"`
	FilePath    string    `json:"file_path" db:"file_path"`
	FileSize    int64     `json:"file_size" db:"file_size"`
	SHA256      string    `json:"sha256" db:"sha256"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}
