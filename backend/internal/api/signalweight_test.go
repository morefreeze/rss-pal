package api

import "testing"

func TestSignalToTopicWeight(t *testing.T) {
	cases := []struct {
		strength float64
		want     float64
	}{
		{2.0, 2.0}, // save
		{1.0, 1.0}, // like
		{0.5, 0.5}, // read>=60
		{0.0, 0.0}, // none
	}
	for _, tc := range cases {
		got := SignalToTopicWeight(tc.strength)
		if got != tc.want {
			t.Errorf("SignalToTopicWeight(%v) = %v; want %v", tc.strength, got, tc.want)
		}
	}
}

func TestSignalToTagWeight(t *testing.T) {
	if got := SignalToTagWeight(2.0); got != 1.0 {
		t.Errorf("save tag weight = %v; want 1.0", got)
	}
	if got := SignalToTagWeight(1.0); got != 0.5 {
		t.Errorf("like tag weight = %v; want 0.5", got)
	}
	if got := SignalToTagWeight(0.0); got != 0.0 {
		t.Errorf("zero tag weight = %v; want 0.0", got)
	}
}
