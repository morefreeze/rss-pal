package api

import "testing"

func TestValidateBriefingTab_Ok(t *testing.T) {
	for _, tab := range []string{"daily", "weekly"} {
		if !ValidateBriefingTab(tab) {
			t.Errorf("ValidateBriefingTab(%q) = false, want true", tab)
		}
	}
}

func TestValidateBriefingTab_Rejects(t *testing.T) {
	for _, tab := range []string{"", "DAILY", "monthly", "  daily "} {
		if ValidateBriefingTab(tab) {
			t.Errorf("ValidateBriefingTab(%q) = true, want false", tab)
		}
	}
}
