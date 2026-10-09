package explore

import (
	"errors"
	"strings"
	"time"
)

// StoppedSourceState keeps admission failures distinct from inaccessible URLs.
func StoppedSourceState(err error) string {
	if errors.Is(err, ErrInactiveSource) || errors.Is(err, ErrInsufficientSourceConfidence) {
		return "ineligible"
	}
	message := err.Error()
	for _, part := range []string{"no supported feed alternate", "no discovered alternate", "at least two parseable", "at least two articles", "no article published", "no parseable articles", "Failed to detect feed type", "document is not a feed"} {
		if strings.Contains(message, part) {
			return "ineligible"
		}
	}
	return "unavailable"
}

// RetryAfter returns a server requested lower bound on the next attempt.
func RetryAfter(err error) time.Time {
	var failure *sourceFetchError
	if errors.As(err, &failure) {
		return failure.retryAfter
	}
	return time.Time{}
}
