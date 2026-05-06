package api

import "testing"

func TestShouldPromptDuplicate(t *testing.T) {
	cases := []struct {
		name     string
		newLen   int
		oldLen   int
		force    bool
		expected bool
	}{
		// force always wins
		{"force overrides everything", 100, 1000, true, false},
		{"force on improvement still passes through", 5000, 1000, true, false},

		// oldLen == 0 means no real prior content; auto-overwrite
		{"old empty, any new", 0, 0, false, false},
		{"old empty, new has content", 100, 0, false, false},

		// 1.5x boundary: clear improvement skips prompt
		{"new exactly 1.5x triggers no prompt", 1500, 1000, false, false},
		{"new just above 1.5x", 1501, 1000, false, false},
		{"new far above 1.5x", 5000, 1000, false, false},

		// below 1.5x prompts
		{"new just below 1.5x prompts", 1499, 1000, false, true},
		{"new equal to old prompts", 1000, 1000, false, true},
		{"new shorter than old prompts", 500, 1000, false, true},
		{"new much shorter prompts", 100, 1000, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldPromptDuplicate(tc.newLen, tc.oldLen, tc.force)
			if got != tc.expected {
				t.Errorf("shouldPromptDuplicate(new=%d, old=%d, force=%v) = %v, want %v",
					tc.newLen, tc.oldLen, tc.force, got, tc.expected)
			}
		})
	}
}
