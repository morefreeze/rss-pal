package explore

import (
	"testing"
	"time"
)

func TestExploreScheduleSlotsAndBoundaries(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	local := func(day int, hour, minute int) time.Time {
		return time.Date(2026, 8, day, hour, minute, 0, 0, shanghai)
	}
	for _, tc := range []struct {
		name       string
		now        time.Time
		hasCurrent bool
		current    time.Time
		next       time.Time
	}{
		{"midnight has no slot", local(31, 0, 0), false, time.Time{}, local(31, 8, 0)},
		{"minute before active window", local(31, 7, 59), false, time.Time{}, local(31, 8, 0)},
		{"08 boundary", local(31, 8, 0), true, local(31, 8, 0), local(31, 11, 0)},
		{"late in 08 slot does not backfill older slots", local(31, 10, 59), true, local(31, 8, 0), local(31, 11, 0)},
		{"11 boundary", local(31, 11, 0), true, local(31, 11, 0), local(31, 14, 0)},
		{"14 boundary", local(31, 14, 0), true, local(31, 14, 0), local(31, 17, 0)},
		{"17 boundary", local(31, 17, 0), true, local(31, 17, 0), local(31, 20, 0)},
		{"20 boundary", local(31, 20, 0), true, local(31, 20, 0), local(31, 23, 0)},
		{"23 boundary", local(31, 23, 0), true, local(31, 23, 0), local(32, 8, 0)},
		{"cross midnight forbids prior-day catchup", local(32, 0, 1), false, time.Time{}, local(32, 8, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schedule := ExploreScheduleAt(tc.now)
			if schedule.HasCurrent != tc.hasCurrent || !schedule.SlotAt.Equal(tc.current) || !schedule.NextSlotAt.Equal(tc.next) {
				t.Fatalf("schedule=%+v want current=%v slot=%v next=%v", schedule, tc.hasCurrent, tc.current, tc.next)
			}
			if tc.hasCurrent && !schedule.ProviderSyncAt.Equal(tc.current.Add(-30*time.Minute)) {
				t.Fatalf("provider sync=%v want=%v", schedule.ProviderSyncAt, tc.current.Add(-30*time.Minute))
			}
			if !schedule.NextProviderSyncAt.Equal(tc.next.Add(-30 * time.Minute)) {
				t.Fatalf("next provider sync=%v want=%v", schedule.NextProviderSyncAt, tc.next.Add(-30*time.Minute))
			}
		})
	}
}

func TestExploreScheduleExposesUpcomingProviderSync(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	schedule := ExploreScheduleAt(time.Date(2026, 8, 31, 10, 0, 0, 0, shanghai))
	want := time.Date(2026, 8, 31, 10, 30, 0, 0, shanghai)
	if !schedule.NextProviderSyncAt.Equal(want) {
		t.Fatalf("next provider sync=%v want=%v", schedule.NextProviderSyncAt, want)
	}
}

func TestExploreScheduleNormalizesInputTimezone(t *testing.T) {
	// 03:15 UTC is 11:15 in Shanghai and belongs only to the 11:00 slot.
	schedule := ExploreScheduleAt(time.Date(2026, 8, 31, 3, 15, 0, 0, time.UTC))
	if !schedule.HasCurrent || schedule.SlotAt.Hour() != 11 || schedule.SlotAt.Location().String() != "Asia/Shanghai" {
		t.Fatalf("schedule=%+v", schedule)
	}
}
