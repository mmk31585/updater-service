package operation

import "testing"

func TestStatusIsTerminal(t *testing.T) {
	tests := []struct {
		status Status
		want   bool
	}{
		{StatusSucceeded, true},
		{StatusFailed, true},
		{StatusRolledBack, true},
		{StatusRollbackFailed, true},
		{StatusPending, false},
		{StatusDispatched, false},
		{StatusTransferred, false},
		{StatusRunning, false},
		{StatusBackupCreated, false},
		{StatusApplying, false},
		{StatusHealthChecking, false},
		{StatusRollingBack, false},
		{StatusRollbackHealthChecking, false},
		{Status("UNKNOWN"), false},
	}

	for _, tc := range tests {
		if got := tc.status.IsTerminal(); got != tc.want {
			t.Errorf("%s.IsTerminal() = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestStatusValues(t *testing.T) {
	statuses := []Status{
		StatusPending,
		StatusDispatched,
		StatusTransferred,
		StatusRunning,
		StatusBackupCreated,
		StatusApplying,
		StatusHealthChecking,
		StatusSucceeded,
		StatusFailed,
		StatusRollingBack,
		StatusRollbackHealthChecking,
		StatusRolledBack,
		StatusRollbackFailed,
	}

	seen := make(map[Status]bool, len(statuses))
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
