package main

import (
	"testing"
	"time"
)

func TestNextDaily0400CST(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			name: "before 4am same day",
			now:  time.Date(2026, 5, 7, 1, 30, 0, 0, cst),
			want: time.Date(2026, 5, 7, 4, 0, 0, 0, cst),
		},
		{
			name: "after 4am next day",
			now:  time.Date(2026, 5, 7, 9, 0, 0, 0, cst),
			want: time.Date(2026, 5, 8, 4, 0, 0, 0, cst),
		},
		{
			name: "exactly 4am next day (target must be strictly after now)",
			now:  time.Date(2026, 5, 7, 4, 0, 0, 0, cst),
			want: time.Date(2026, 5, 8, 4, 0, 0, 0, cst),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextDaily0400CST(tc.now)
			if !got.Equal(tc.want) {
				t.Errorf("nextDaily0400CST(%v) = %v; want %v", tc.now, got, tc.want)
			}
		})
	}
}
