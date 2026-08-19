package app

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// RetryTransport wraps an http.RoundTripper and retries transient failures
// (429, 502, 503, 504, network errors) with exponential backoff.
type RetryTransport struct {
	Base      http.RoundTripper
	MaxRetry  int
	Logger    *slog.Logger
}

func (t *RetryTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *RetryTransport) maxRetry() int {
	if t.MaxRetry > 0 {
		return t.MaxRetry
	}
	return 2
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var lastResp *http.Response
	var lastErr error
	maxRetry := t.maxRetry()

	// Buffer request body for retries (only for methods that have a body)
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	for attempt := 0; attempt <= maxRetry; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s, 4s
			if t.Logger != nil {
				t.Logger.Warn("retrying HTTP request",
					"attempt", attempt,
					"max_retry", maxRetry,
					"url", req.URL.Host+req.URL.Path,
					"backoff", backoff,
				)
			}
			time.Sleep(backoff)
		}

		// Restore request body for retry
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := t.base().RoundTrip(req)
		if err != nil {
			lastErr = err
			continue
		}

		if !isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Drain and close retryable response
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		lastResp = resp
	}

	if lastResp != nil {
		return lastResp, nil
	}
	return nil, lastErr
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,      // 429
		http.StatusBadGateway,            // 502
		http.StatusServiceUnavailable,    // 503
		http.StatusGatewayTimeout:        // 504
		return true
	default:
		return false
	}
}
