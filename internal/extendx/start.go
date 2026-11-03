package extendx

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	sdkcore "github.com/extend-hq/extend-go-sdk/core"
	"github.com/extend-hq/extend-go-sdk/option"
)

const (
	maxRateLimitRetries = 2
	maxRateLimitDelay   = time.Minute
)

// StartOnce calls an SDK method that starts work on the server, such as
// creating a run or a batch. The SDK's own retries are off for it: after a
// 408 or 5xx the server may already have started the work, and a retry
// would start it again. A 429 is retried after the wait the server asks
// for, since the API turns rate-limited requests away before doing
// anything.
func StartOnce[T any](ctx context.Context, call func(...option.RequestOption) (T, error)) (T, error) {
	for attempt := 0; ; attempt++ {
		result, err := call(option.WithoutRetries())
		if err == nil || attempt == maxRateLimitRetries {
			return result, err
		}
		delay, ok := rateLimitDelay(err, attempt)
		if !ok {
			return result, err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			var zero T
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}

// rateLimitDelay reports whether err is a 429 and how long to wait before
// the next attempt: the server's Retry-After when it sends one, otherwise
// an exponential backoff starting at one second.
func rateLimitDelay(err error, attempt int) (time.Duration, bool) {
	var apiErr *sdkcore.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	delay := time.Second << attempt
	if v := apiErr.Header.Get("Retry-After"); v != "" {
		if secs, perr := strconv.Atoi(v); perr == nil && secs >= 0 {
			delay = time.Duration(secs) * time.Second
		} else if at, perr := http.ParseTime(v); perr == nil {
			delay = max(time.Until(at), 0)
		}
	}
	return min(delay, maxRateLimitDelay), true
}
