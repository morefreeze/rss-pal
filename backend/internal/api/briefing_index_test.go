package api

import (
	"testing"
	"time"
)

func TestParseBriefingIndexType_Valid(t *testing.T) {
	if got, err := parseBriefingIndexType("daily"); err != nil || got != "daily" {
		t.Errorf("daily: got (%q, %v)", got, err)
	}
	if got, err := parseBriefingIndexType("weekly"); err != nil || got != "weekly" {
		t.Errorf("weekly: got (%q, %v)", got, err)
	}
}

func TestParseBriefingIndexType_Invalid(t *testing.T) {
	for _, in := range []string{"", "DAILY", "monthly", "day"} {
		if _, err := parseBriefingIndexType(in); err == nil {
			t.Errorf("%q: expected error", in)
		}
	}
}

func TestParseBriefingIndexRange_Valid(t *testing.T) {
	from, to, err := parseBriefingIndexRange("2026-05-01", "2026-05-31")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantFrom := time.Date(2026, 5, 1, 0, 0, 0, 0, briefingShanghai)
	wantTo := time.Date(2026, 5, 31, 0, 0, 0, 0, briefingShanghai)
	if !from.Equal(wantFrom) || !to.Equal(wantTo) {
		t.Errorf("got (%s, %s), want (%s, %s)", from, to, wantFrom, wantTo)
	}
}

func TestParseBriefingIndexRange_FromAfterTo(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("2026-06-01", "2026-05-01"); err == nil {
		t.Error("expected error when from > to")
	}
}

func TestParseBriefingIndexRange_TooWide(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("2025-04-25", "2026-06-01"); err == nil {
		t.Error("expected error on 400+ day span")
	}
}

func TestParseBriefingIndexRange_Empty(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("", "2026-05-31"); err == nil {
		t.Error("expected error on empty from")
	}
	if _, _, err := parseBriefingIndexRange("2026-05-01", ""); err == nil {
		t.Error("expected error on empty to")
	}
}

func TestParseBriefingIndexRange_BadFormat(t *testing.T) {
	if _, _, err := parseBriefingIndexRange("2026/05/01", "2026-05-31"); err == nil {
		t.Error("expected error on slash format")
	}
}
