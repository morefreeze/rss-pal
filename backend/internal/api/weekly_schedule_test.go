package api

import (
	"testing"
	"time"
)

func weeklyTestTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, shanghai)
}

func TestWeeklyGenerationMetadata(t *testing.T) {
	weekStart := weeklyTestTime(2026, time.August, 24, 0, 0)
	tests := []struct {
		name        string
		now         time.Time
		cached      bool
		wantStatus  WeeklyGenerationStatus
		wantPending bool
		wantETA     string
	}{
		{"before schedule", weeklyTestTime(2026, time.August, 25, 12, 0), false, WeeklyGenerationScheduled, true, "2026-08-31T05:00:00+08:00"},
		{"at schedule", weeklyTestTime(2026, time.August, 31, 5, 0), false, WeeklyGenerationPending, true, ""},
		{"second completed week", weeklyTestTime(2026, time.September, 7, 8, 0), false, WeeklyGenerationPending, true, ""},
		{"outside catch-up", weeklyTestTime(2026, time.September, 14, 8, 0), false, WeeklyGenerationNotPlanned, false, ""},
		{"cached wins", weeklyTestTime(2026, time.September, 14, 8, 0), true, WeeklyGenerationReady, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WeeklyGenerationMetadataAt(tt.now, weekStart, tt.cached)
			if got.Status != tt.wantStatus || got.Pending != tt.wantPending {
				t.Fatalf("metadata = %+v, want status=%s pending=%v", got, tt.wantStatus, tt.wantPending)
			}
			gotETA := ""
			if got.EstimatedGenerationAt != nil {
				gotETA = got.EstimatedGenerationAt.Format(time.RFC3339)
			}
			if gotETA != tt.wantETA {
				t.Fatalf("eta = %q, want %q", gotETA, tt.wantETA)
			}
		})
	}
}

func TestRecentCompletedWeekStartsUsesSharedWindow(t *testing.T) {
	got := RecentCompletedWeekStarts(weeklyTestTime(2026, time.August, 25, 10, 0), WeeklyCatchUpWeekCount)
	want := []time.Time{
		weeklyTestTime(2026, time.August, 17, 0, 0),
		weeklyTestTime(2026, time.August, 10, 0, 0),
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("week[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}
