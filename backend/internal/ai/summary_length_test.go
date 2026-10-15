package ai

import (
	"context"
	"strings"
	"testing"
)

func assertSummaryLengthPrompts(t *testing.T, bodies [][]byte) {
	t.Helper()
	for i, body := range bodies {
		prompt := requestUserText(t, body)
		want := "总字符数不超过300"
		if i%2 == 1 {
			want = "总字符数不超过2000"
		}
		for _, rule := range []string{want, "完整收尾", "已有更严格的篇幅限制", "Markdown"} {
			if !strings.Contains(prompt, rule) {
				t.Errorf("request %d missing %q", i, rule)
			}
		}
	}
}

func TestSummaryLengthInstructionsReachTextRequests(t *testing.T) {
	srv, cap := newCaptureServer(t)
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
	if _, err := s.Summarize(context.Background(), "Title", articleAnchorTestContent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SummarizeWithTemplate(context.Background(), "Title", articleAnchorTestContent, "100字以内 {content}", "300字以内 {content}"); err != nil {
		t.Fatal(err)
	}
	if len(cap.bodies) != 4 {
		t.Fatalf("requests=%d", len(cap.bodies))
	}
	assertSummaryLengthPrompts(t, cap.bodies)
	if !strings.Contains(requestUserText(t, cap.bodies[3]), "300字以内") {
		t.Fatal("stricter template lost")
	}
}

func TestSummaryLengthInstructionsReachStreamingRequests(t *testing.T) {
	srv, cap := newStreamCaptureServer(t)
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
	delta := func(string) {}
	if _, err := s.SummarizeStream(context.Background(), "Title", articleAnchorTestContent, delta, delta); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SummarizeWithTemplateStream(context.Background(), "Title", articleAnchorTestContent, "{content}", "{content}", delta, delta); err != nil {
		t.Fatal(err)
	}
	if len(cap.bodies) != 4 {
		t.Fatalf("requests=%d", len(cap.bodies))
	}
	assertSummaryLengthPrompts(t, cap.bodies)
}

func TestSummaryLengthInstructionsInVisionPrompts(t *testing.T) {
	for _, urls := range []string{"", "https://example.com/pic.jpg"} {
		for _, tc := range []struct{ prompt, rule string }{
			{buildVisionBriefPrompt("Title", "Body", urls), "总字符数不超过300"},
			{buildVisionDetailedPrompt("Title", "Body", urls), "总字符数不超过2000"},
		} {
			if !strings.Contains(tc.prompt, tc.rule) || !strings.Contains(tc.prompt, "完整收尾") {
				t.Errorf("vision prompt missing length/ending rules: %s", tc.prompt)
			}
		}
	}
}
