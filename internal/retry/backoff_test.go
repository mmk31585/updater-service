package retry

import (
	"testing"
	"time"
)

func TestBackoffDelay(t *testing.T) {
	tests := []struct {
		name     string
		initial  time.Duration
		max      time.Duration
		retryNum int
		want     time.Duration
	}{
		{"retry_zero_returns_initial", 1 * time.Second, 10 * time.Second, 0, 1 * time.Second},
		{"retry_one_doubles", 1 * time.Second, 10 * time.Second, 1, 2 * time.Second},
		{"retry_two_doubles_again", 1 * time.Second, 10 * time.Second, 2, 4 * time.Second},
		{"retry_three", 1 * time.Second, 10 * time.Second, 3, 8 * time.Second},
		{"capped_at_max", 1 * time.Second, 4 * time.Second, 3, 4 * time.Second},
		{"large_initial_capped", 5 * time.Second, 10 * time.Second, 2, 10 * time.Second},
		{"max_zero_returns_zero", 1 * time.Second, 0, 3, 0},
		{"single_retry_at_max", 1 * time.Second, 1 * time.Second, 1, 1 * time.Second},
		{"negative_retry_num", 1 * time.Second, 10 * time.Second, -1, 1 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Backoff{Initial: tt.initial, Max: tt.max}
			got := b.Delay(tt.retryNum)
			if got != tt.want {
				t.Errorf("Backoff.Delay(%d) = %v, want %v", tt.retryNum, got, tt.want)
			}
		})
	}
}

func TestBackoffDelayExactSequence(t *testing.T) {
	b := Backoff{Initial: 100 * time.Millisecond, Max: 800 * time.Millisecond}

	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		800 * time.Millisecond,
	}

	for i, want := range expected {
		got := b.Delay(i)
		if got != want {
			t.Errorf("Delay(%d) = %v, want %v", i, got, want)
		}
	}
}

func TestBackoffDelayMaxEqualsInitial(t *testing.T) {
	b := Backoff{Initial: 1 * time.Second, Max: 1 * time.Second}
	got := b.Delay(5)
	if got != 1*time.Second {
		t.Errorf("Delay(5) with Initial=Max=1s = %v, want %v", got, 1*time.Second)
	}
}

func TestBackoffDelayNoMax(t *testing.T) {
	b := Backoff{Initial: 100 * time.Millisecond, Max: 0}
	got := b.Delay(4)
	// When Max is 0, any delay >= 0 triggers the cap, so it returns 0
	if got != 0 {
		t.Errorf("Delay(4) with Max=0 = %v, want 0", got)
	}
}

func TestBackoffDelayRetryZero(t *testing.T) {
	b := Backoff{Initial: 5 * time.Second, Max: 100 * time.Second}
	got := b.Delay(0)
	if got != 5*time.Second {
		t.Errorf("Delay(0) = %v, want %v", got, 5*time.Second)
	}
}
