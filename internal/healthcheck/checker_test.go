package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmk31585/updater-service/internal/retry"
)

func testConfig() Config {
	return Config{
		TotalTimeout:   2 * time.Second,
		RequestTimeout: 500 * time.Millisecond,
		MaxRetries:     3,
		Backoff:        retry.Backoff{Initial: time.Millisecond, Max: 5 * time.Millisecond},
	}
}

func TestWaitUntilHealthy(t *testing.T) {
	t.Run("healthy immediately", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := NewChecker(testConfig()).WaitUntilHealthy(context.Background(), srv.URL); err != nil {
			t.Fatalf("WaitUntilHealthy() error = %v", err)
		}
	})

	t.Run("healthy after retries", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) < 3 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		if err := NewChecker(testConfig()).WaitUntilHealthy(context.Background(), srv.URL); err != nil {
			t.Fatalf("WaitUntilHealthy() error = %v", err)
		}
		if got := calls.Load(); got != 3 {
			t.Errorf("calls = %d, want 3", got)
		}
	})

	t.Run("never healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		if err := NewChecker(testConfig()).WaitUntilHealthy(context.Background(), srv.URL); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		if err := NewChecker(testConfig()).WaitUntilHealthy(context.Background(), "://bad"); err == nil {
			t.Fatal("expected error for invalid url, got nil")
		}
	})

	t.Run("request timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		cfg := testConfig()
		cfg.RequestTimeout = 20 * time.Millisecond
		cfg.MaxRetries = 0

		start := time.Now()
		if err := NewChecker(cfg).WaitUntilHealthy(context.Background(), srv.URL); err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("timeout took %s, expected well under 1s", elapsed)
		}
	})

	t.Run("respects cancelled context", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if err := NewChecker(testConfig()).WaitUntilHealthy(ctx, srv.URL); err == nil {
			t.Fatal("expected error for cancelled context, got nil")
		}
	})
}
