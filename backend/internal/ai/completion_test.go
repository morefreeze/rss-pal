package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompletionRetriesLengthWithLargerBudget(t *testing.T) {
	var budgets []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		budgets = append(budgets, req.MaxTokens)
		reason, text := "length", "桌游玩家"
		if len(budgets) == 2 {
			reason, text = "stop", "完整总结。"
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q},"finish_reason":%q}]}`, text, reason)
	}))
	defer srv.Close()
	s := NewSummarizerWithModel("k", srv.URL, "m")
	got, err := s.call(context.Background(), "prompt", 4000)
	if err != nil || got != "完整总结。" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(budgets) != 2 || budgets[0] != 4000 || budgets[1] != 8000 {
		t.Fatalf("budgets=%v", budgets)
	}
}

func TestCompletionNeverAcceptsRepeatedLength(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"choices":[{"message":{"content":"半截"},"finish_reason":"length"}]}`)
	}))
	defer srv.Close()
	s := NewSummarizerWithModel("k", srv.URL, "m")
	got, err := s.call(context.Background(), "prompt", 4000)
	if err == nil || got != "" {
		t.Fatalf("accepted partial: %q, %v", got, err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want bounded 2", calls)
	}
}

func TestCompletionStreamRejectsTruncation(t *testing.T) {
	for _, ending := range []string{
		`data: {"choices":[{"delta":{},"finish_reason":"length"}]}` + "\n\ndata: [DONE]\n\n",
		"",
	} {
		t.Run(fmt.Sprint(len(ending)), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"半截\"}}]}\n\n"+ending)
			}))
			defer srv.Close()
			s := NewSummarizerWithModel("k", srv.URL, "m")
			got, err := s.callStream(context.Background(), "p", 4000, func(string) {})
			if err == nil {
				t.Fatalf("accepted stream: %q", got)
			}
		})
	}
}

func TestCompletionRejectsContentFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"部分"},"finish_reason":"content_filter"}]}`)
	}))
	defer srv.Close()
	s := NewSummarizerWithModel("k", srv.URL, "m")
	_, err := s.call(context.Background(), "prompt", 4000)
	if err == nil || !strings.Contains(err.Error(), "content_filter") {
		t.Fatalf("err=%v", err)
	}
}

func TestAnthropicLengthFinishIsPreserved(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if stream {
					fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"model\":\"m\"}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"半截\"}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
				} else {
					fmt.Fprint(w, `{"type":"message","model":"m","content":[{"type":"text","text":"半截"}],"stop_reason":"max_tokens"}`)
				}
			}))
			defer srv.Close()
			s := NewSummarizerWithModel("k", srv.URL, "m")
			s.providerProtocol = "anthropic"
			var err error
			if stream {
				_, err = s.callStream(context.Background(), "p", 4000, func(string) {})
			} else {
				_, err = s.call(context.Background(), "p", 4000)
			}
			if err == nil {
				t.Fatal("accepted token-limited native response")
			}
			wantCalls := 2
			if stream {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
