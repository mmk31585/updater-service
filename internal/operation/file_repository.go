package operation

import "context"

type FileRepository interface {
	Create(ctx context.Context, meta FileMetadata) error
	Get(ctx context.Context, operationID string) (FileMetadata, error)
}
