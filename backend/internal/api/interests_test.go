package api

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestNewInterestLatestResponseUsesRequestedPayloadKey(t *testing.T) {
	interest := &model.UserInterest{ID: 7}
	quota := interestQuota{RemainingToday: 2, RemainingMonth: 99}

	canonical := newInterestLatestResponse("interest", interest, quota)
	if got := canonical["interest"]; got != interest {
		t.Fatalf("canonical interest = %#v, want %#v", got, interest)
	}
	if _, ok := canonical["insight"]; ok {
		t.Fatal("canonical response unexpectedly contains insight")
	}

	legacy := newInterestLatestResponse("insight", interest, quota)
	if got := legacy["insight"]; got != interest {
		t.Fatalf("legacy insight = %#v, want %#v", got, interest)
	}
	if _, ok := legacy["interest"]; ok {
		t.Fatal("legacy response unexpectedly contains interest")
	}
}
