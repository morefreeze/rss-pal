package api

import (
	"testing"
	"time"
)

func sh(year int, month time.Month, day, hour int) time.Time {
	return time.Date(year, month, day, hour, 0, 0, 0, briefingShanghai)
}

func TestTodayLabel_Before5amBelongsToYesterday(t *testing.T) {
	now := sh(2026, 5, 29, 3)
	got := TodayLabel(now)
	want := time.Date(2026, 5, 28, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTodayLabel_AtOrAfter5amBelongsToToday(t *testing.T) {
	now := sh(2026, 5, 29, 5)
	got := TodayLabel(now)
	want := time.Date(2026, 5, 29, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestTodayLabel_AcrossUTCDayInsideShanghaiDay(t *testing.T) {
	// 2026-05-29 23:30 UTC == 2026-05-30 07:30 Shanghai → label 2026-05-30
	now := time.Date(2026, 5, 29, 23, 30, 0, 0, time.UTC)
	got := TodayLabel(now)
	want := time.Date(2026, 5, 30, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseDailyDate_Valid(t *testing.T) {
	got, err := ParseDailyDate("2026-05-29")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := time.Date(2026, 5, 29, 0, 0, 0, 0, briefingShanghai)
	if !got.Equal(want) {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestParseDailyDate_Invalid(t *testing.T) {
	if _, err := ParseDailyDate("2026/05/29"); err == nil {
		t.Error("expected err on slash format")
	}
	if _, err := ParseDailyDate(""); err == nil {
		t.Error("expected err on empty")
	}
}

func TestDailyWindow_StartsAt5amEndsAt5amNextDay(t *testing.T) {
	day := time.Date(2026, 5, 28, 0, 0, 0, 0, briefingShanghai)
	start, end := DailyWindow(day)
	wantStart := time.Date(2026, 5, 28, 5, 0, 0, 0, briefingShanghai)
	wantEnd := time.Date(2026, 5, 29, 5, 0, 0, 0, briefingShanghai)
	if !start.Equal(wantStart) {
		t.Errorf("start = %s, want %s", start, wantStart)
	}
	if !end.Equal(wantEnd) {
		t.Errorf("end = %s, want %s", end, wantEnd)
	}
}
