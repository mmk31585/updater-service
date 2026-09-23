package retry

import "time"

type Backoff struct {
	Initial time.Duration
	Max     time.Duration
}

func (b Backoff) Delay(retryNumber int) time.Duration {
	if retryNumber <= 0 {
		return b.Initial
	}

	delay := b.Initial

	for i := 0; i < retryNumber; i++ {
		delay *= 2

		if delay >= b.Max {
			return b.Max
		}
	}

	return delay
}
