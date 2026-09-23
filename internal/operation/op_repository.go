package operation

import "context"

type OperationRepository interface {
	Create(ctx context.Context, op Operation) error
	Get(ctx context.Context, id string) (Operation, error)
	AdvanceStatus(ctx context.Context, id string, status Status) (bool, error)
	ClaimForExecution(
		ctx context.Context,
		id string,
	) (bool, Operation, error)
	ListAll(ctx context.Context) ([]Operation, error)
}
