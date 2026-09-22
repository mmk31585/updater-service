package operation

import "sync"

type Store struct {
	mu         sync.RWMutex
	operations map[string]Operation
}

func NewStore() *Store {
	return &Store{
		operations: make(map[string]Operation),
	}
}

func (s *Store) Create(op Operation) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.operations[op.ID] = op
}

func (s *Store) Get(id string) (Operation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	op, ok := s.operations[id]

	return op, ok
}

func (s *Store) UpdateStatus(
	id string,
	status Status,
) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	op, ok := s.operations[id]
	if !ok {
		return false
	}

	op.Status = status
	s.operations[id] = op

	return true
}