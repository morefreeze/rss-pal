package registrationpolicy

import "testing"

func TestShareOwners(t *testing.T) {
	for _, tc := range []struct {
		mode, owners string
		id           int
		want         bool
	}{
		{"selected_owners", "1", 1, true}, {"selected_owners", "1", 2, false},
		{"selected_owners", "", 1, false}, {"all", "", 2, true}, {"disabled", "1", 1, false},
		{"invalid", "1", 1, false}, {"selected_owners", "1,broken", 1, false}, {"selected_owners", "1,-2", 1, false},
		{"selected_owners", "1, 3", 3, true}, {"all", "", 0, false},
	} {
		if got := Parse(tc.mode, tc.owners).Allows(tc.id); got != tc.want {
			t.Errorf("%+v got %v", tc, got)
		}
	}
}
