package main

import (
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/api"
)

func sh(year int, month time.Month, day, hour, min int) time.Time {
	loc := time.FixedZone("Asia/Shanghai", 8*3600)
	return time.Date(year, month, day, hour, min, 0, 0, loc)
}

func TestNextBriefingFire_BeforeFive(t *testing.T) {
	now := sh(2026, 5, 29, 3, 0)
	got := nextBriefingFire(now)
	want := sh(2026, 5, 29, 5, 0)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNextBriefingFire_AtFive(t *testing.T) {
	now := sh(2026, 5, 29, 5, 0)
	got := nextBriefingFire(now)
	want := sh(2026, 5, 30, 5, 0)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestNextBriefingFire_AfterFive(t *testing.T) {
	now := sh(2026, 5, 29, 14, 30)
	got := nextBriefingFire(now)
	want := sh(2026, 5, 30, 5, 0)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestIsMondayInShanghai(t *testing.T) {
	mon := sh(2026, 5, 25, 5, 0) // 2026-05-25 was a Monday
	if !isMondayShanghai(mon) {
		t.Errorf("expected Mon for %s", mon)
	}
	tue := sh(2026, 5, 26, 5, 0)
	if isMondayShanghai(tue) {
		t.Errorf("expected !Mon for %s", tue)
	}
}

func TestRecentCompletedWeekStarts(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
	}{
		{name: "midweek", now: sh(2026, time.August, 25, 10, 0)},
		{name: "monday boundary", now: sh(2026, time.August, 24, 0, 1)},
	}
	want := []time.Time{
		sh(2026, time.August, 17, 0, 0),
		sh(2026, time.August, 10, 0, 0),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := api.RecentCompletedWeekStarts(tt.now, api.WeeklyCatchUpWeekCount)
			if len(got) != len(want) {
				t.Fatalf("len = %d, want %d", len(got), len(want))
			}
			for i := range want {
				if !got[i].Equal(want[i]) {
					t.Errorf("week[%d] = %s, want %s", i, got[i], want[i])
				}
			}
		})
	}
}
