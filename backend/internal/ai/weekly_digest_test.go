package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateWeeklyIntroUsesWeeklyTokenBudget(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"本周主题导语"}}]}`)
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("key", srv.URL, "model")
	_, err := s.GenerateWeeklyIntro(context.Background(), []WeeklyDigestItem{{
		Title:        "文章",
		SummaryBrief: "摘要",
	}})
	if err != nil {
		t.Fatalf("GenerateWeeklyIntro: %v", err)
	}
	if got.MaxTokens != 4096 {
		t.Fatalf("max_tokens = %d, want 4096", got.MaxTokens)
	}
}
