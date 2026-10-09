package explore

import (
	"testing"
	"time"
)

func TestTransientAccessStatusesAreRetryable(t *testing.T) {
	for _, status := range []int{401, 403, 404, 408, 425, 429, 500, 503} {
		if got := classifyHTTPStatus(status); got != SourceFetchRetryable {
			t.Errorf("HTTP %d classified %s", status, got)
		}
	}
	if got := classifyHTTPStatus(410); got != SourceFetchTerminal {
		t.Errorf("410 classified %s", got)
	}
}

func TestInactivityStopsAutomaticAttempts(t *testing.T) {
	if got := ClassifySourceFetchError(ErrInactiveSource); got != SourceFetchTerminal {
		t.Fatalf("inactive source classified %s", got)
	}
}

func TestRetryAfterServerDelay(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	for _, value := range []string{"7200", "Fri, 09 Oct 2026 02:00:00 GMT"} {
		if got := parseRetryAfter(value, now); !got.Equal(now.Add(2 * time.Hour)) {
			t.Fatalf("%s: %v", value, got)
		}
	}
	for _, value := range []string{"-1", "9999999999999999999", "invalid"} {
		if !parseRetryAfter(value, now).IsZero() {
			t.Fatalf("accepted %s", value)
		}
	}
}
