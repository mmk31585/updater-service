package node

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

type fakeScanner struct {
	values []any
	err    error
}

func (s *fakeScanner) Scan(dest ...any) error {
	if s.err != nil {
		return s.err
	}
	if len(dest) != len(s.values) {
		return errors.New("scan arity mismatch")
	}

	for i, d := range dest {
		v := s.values[i]
		switch ptr := d.(type) {
		case *string:
			*ptr = v.(string)
		case *sql.NullString:
			*ptr = v.(sql.NullString)
		case *time.Time:
			*ptr = v.(time.Time)
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}

func TestScanNode(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("valid row with capabilities", func(t *testing.T) {
		scanner := &fakeScanner{values: []any{
			"node-1",
			"instance-1",
			"ONLINE",
			"node-1:8080",
			"dev",
			sql.NullString{String: `["config-service"]`, Valid: true},
			now,
			now.Add(time.Minute),
			now,
			now,
			now,
		}}

		record, err := scanNode(scanner)
		if err != nil {
			t.Fatalf("scanNode() error = %v", err)
		}

		if record.ID != "node-1" || record.InstanceID != "instance-1" {
			t.Errorf("unexpected identity: %+v", record)
		}
		if record.Status != StatusOnline {
			t.Errorf("Status = %q, want %q", record.Status, StatusOnline)
		}
		if len(record.Capabilities) != 1 || record.Capabilities[0] != "config-service" {
			t.Errorf("Capabilities = %v, want [config-service]", record.Capabilities)
		}
	})

	t.Run("empty capabilities", func(t *testing.T) {
		scanner := &fakeScanner{values: []any{
			"node-1", "instance-1", "OFFLINE", "", "", sql.NullString{},
			now, now, now, now, now,
		}}

		record, err := scanNode(scanner)
		if err != nil {
			t.Fatalf("scanNode() error = %v", err)
		}
		if record.Capabilities != nil {
			t.Errorf("Capabilities = %v, want nil", record.Capabilities)
		}
	})

	t.Run("invalid capabilities json", func(t *testing.T) {
		scanner := &fakeScanner{values: []any{
			"node-1", "instance-1", "ONLINE", "", "",
			sql.NullString{String: "not-json", Valid: true},
			now, now, now, now, now,
		}}

		if _, err := scanNode(scanner); err == nil {
			t.Fatal("expected error for invalid capabilities json, got nil")
		}
	})

	t.Run("scan error propagates", func(t *testing.T) {
		wantErr := errors.New("boom")
		scanner := &fakeScanner{err: wantErr}

		_, err := scanNode(scanner)
		if !errors.Is(err, wantErr) {
			t.Fatalf("scanNode() error = %v, want %v", err, wantErr)
		}
	})
}

func TestStatusValuesDistinct(t *testing.T) {
	statuses := []Status{StatusOnline, StatusDraining, StatusOffline}
	seen := make(map[Status]bool)
	for _, status := range statuses {
		if status == "" {
			t.Error("status must not be empty")
		}
		if seen[status] {
			t.Errorf("duplicate status %q", status)
		}
		seen[status] = true
	}
}
