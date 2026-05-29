package main

import (
	"testing"
	"time"
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
