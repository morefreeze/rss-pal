package api

import (
	"testing"
	"time"
)

func TestWeeklyMissingResponseScheduled(t *testing.T) {
	response := weeklyMissingResponse(
		weeklyTestTime(2026, time.August, 25, 12, 0),
		weeklyTestTime(2026, time.August, 24, 0, 0),
	)

	if response["week_start"] != "2026-08-24" {
		t.Fatalf("week_start = %v", response["week_start"])
	}
	if response["pending"] != true {
		t.Fatalf("pending = %v", response["pending"])
	}
	if response["generation_status"] != WeeklyGenerationScheduled {
		t.Fatalf("generation_status = %v", response["generation_status"])
	}
	if response["estimated_generation_at"] != "2026-08-31T05:00:00+08:00" {
		t.Fatalf("estimated_generation_at = %v", response["estimated_generation_at"])
	}
}
