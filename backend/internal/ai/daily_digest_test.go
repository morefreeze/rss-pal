package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDailyDigestJSON_HappyPath(t *testing.T) {
	raw := `{"picks":[0,2,3,7,11],"intro":"` + strings.Repeat("某种主题", 25) + `"}`
	picks, intro, err := ParseDailyDigestJSON(raw, 20, 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(picks) != 5 {
		t.Fatalf("picks len = %d", len(picks))
	}
	wantOrder := []int{0, 2, 3, 7, 11}
	for i, p := range picks {
		if p != wantOrder[i] {
			t.Errorf("picks[%d] = %d want %d", i, p, wantOrder[i])
		}
	}
	if !strings.Contains(intro, "某种主题") {
		t.Errorf("intro lost: %q", intro)
	}
}

func TestParseDailyDigestJSON_FenceWrapped(t *testing.T) {
	body := `{"picks":[1,2,3,4,5],"intro":"` + strings.Repeat("主题", 50) + `"}`
	raw := "```json\n" + body + "\n```"
	picks, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(picks) != 5 {
		t.Errorf("picks len = %d", len(picks))
	}
}

func TestParseDailyDigestJSON_WrongPickCount(t *testing.T) {
	raw := `{"picks":[0,1,2],"intro":"` + strings.Repeat("文章", 50) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on wrong pick count")
	}
}

func TestParseDailyDigestJSON_DuplicateIndex(t *testing.T) {
	raw := `{"picks":[0,0,1,2,3],"intro":"` + strings.Repeat("文章", 50) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on duplicate")
	}
}

func TestParseDailyDigestJSON_IndexOutOfRange(t *testing.T) {
	raw := `{"picks":[0,1,2,3,99],"intro":"` + strings.Repeat("文章", 50) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on out-of-range")
	}
}

func TestParseDailyDigestJSON_IntroTooShort(t *testing.T) {
	raw := `{"picks":[0,1,2,3,4],"intro":"太短了"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on short intro")
	}
}

func TestParseDailyDigestJSON_IntroTooLong(t *testing.T) {
	raw := `{"picks":[0,1,2,3,4],"intro":"` + strings.Repeat("字", 300) + `"}`
	_, _, err := ParseDailyDigestJSON(raw, 20, 5)
	if err == nil {
		t.Fatal("expected err on long intro")
	}
}

func TestParseDailyDigestJSON_Malformed(t *testing.T) {
	_, _, err := ParseDailyDigestJSON("not json", 20, 5)
	if err == nil {
		t.Fatal("expected err on garbage")
	}
}

func TestParseDailyDigestJSON_DynamicN(t *testing.T) {
	raw := `{"picks":[0,1,2],"intro":"` + strings.Repeat("文", 100) + `"}`
	picks, _, err := ParseDailyDigestJSON(raw, 3, 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(picks) != 3 {
		t.Errorf("picks len = %d", len(picks))
	}
}

func TestBuildDailyPrompt_IncludesCandidatesAndN(t *testing.T) {
	cands := []DailyCandidate{
		{Idx: 0, Title: "A", SummaryBrief: "brief A"},
		{Idx: 1, Title: "B", SummaryBrief: "brief B"},
	}
	prompt := BuildDailyPrompt(cands, 2)
	if !strings.Contains(prompt, "[0] 《A》") || !strings.Contains(prompt, "[1] 《B》") {
		t.Errorf("prompt missing candidate lines: %q", prompt)
	}
	if !strings.Contains(prompt, "精选 2 篇") {
		t.Errorf("prompt missing N=2: %q", prompt)
	}
}

func TestGenerateDailyDigest_HappyPath(t *testing.T) {
	body := `{"picks":[0,1,2,3,4],"intro":"` + strings.Repeat("主题", 50) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q}}]}`, body)
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("k", srv.URL, "m")
	cands := make([]DailyCandidate, 20)
	for i := range cands {
		cands[i] = DailyCandidate{Idx: i, Title: fmt.Sprintf("T%d", i), SummaryBrief: "s"}
	}
	picks, intro, err := s.GenerateDailyDigest(context.Background(), cands)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(picks) != 5 {
		t.Errorf("picks len = %d", len(picks))
	}
	if intro == "" {
		t.Errorf("intro empty")
	}
}

func TestGenerateDailyDigest_EmptyReturnsNil(t *testing.T) {
	s := NewSummarizerWithModel("k", "http://localhost:1", "m") // never called
	picks, intro, err := s.GenerateDailyDigest(context.Background(), nil)
	if err != nil || picks != nil || intro != "" {
		t.Errorf("want (nil,\"\",nil); got (%v,%q,%v)", picks, intro, err)
	}
}
