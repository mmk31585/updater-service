package operation

import "context"

type Repository interface {
	Create(ctx context.Context, op Operation) error
	Get(ctx context.Context, id string) (Operation, error)
	AdvanceStatus(ctx context.Context, id string, status Status) (bool, error)
}
