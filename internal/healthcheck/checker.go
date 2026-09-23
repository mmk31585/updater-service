package healthcheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mmk31585/updater-service/internal/retry"
)

type Config struct {
	TotalTimeout   time.Duration
	RequestTimeout time.Duration
	MaxRetries     int
	Backoff        retry.Backoff
}

type Checker struct {
	client *http.Client
	config Config
}

func NewChecker(config Config) *Checker {
	return &Checker{
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
		config: config,
	}
}

func (c *Checker) WaitUntilHealthy(
	ctx context.Context,
	url string,
) error {
	checkCtx, cancel := context.WithTimeout(
		ctx,
		c.config.TotalTimeout,
	)
	defer cancel()

	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		err := c.check(checkCtx, url)

		if err == nil {
			return nil
		}

		lastErr = err

		if attempt == c.config.MaxRetries {
			break
		}

		delay := c.config.Backoff.Delay(attempt)

		timer := time.NewTimer(delay)

		select {
		case <-checkCtx.Done():
			return fmt.Errorf(
				"health check timeout: %w",
				lastErr,
			)

		case <-timer.C:
		}
	}

	if lastErr != nil {
		return fmt.Errorf(
			"health check failed after %d retries: %w",
			c.config.MaxRetries,
			lastErr,
		)
	}

	return fmt.Errorf("health check failed")
}

func (c *Checker) check(
	ctx context.Context,
	url string,
) error {
	requestCtx, cancel := context.WithTimeout(
		ctx,
		c.config.RequestTimeout,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"health endpoint returned status %d",
			resp.StatusCode,
		)
	}

	return nil
}
