package summaryeval

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bytedance/rss-pal/internal/taskbudget"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func wireResponse(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func testRequest(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(withScope(context.Background(), "generation", "017", Default), "POST", "https://provider.example/chat/completions", strings.NewReader(`{"model":"requested","messages":[{"role":"user","content":"source"}],"max_tokens":100}`))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func consume(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestTransportBudgetsEveryPhysicalRequestAndCapturesOnlySafeMetadata(t *testing.T) {
	var physical atomic.Int32
	tr := NewBudgetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		physical.Add(1)
		return wireResponse(`{"model":"actual-v2","choices":[{"finish_reason":"stop","message":{"content":"safe"}}],"usage":{"prompt_tokens":11,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":2}}}`), nil
	}), 2)
	for i := 0; i < 2; i++ {
		r, err := tr.RoundTrip(testRequest(t))
		if err != nil {
			t.Fatal(err)
		}
		consume(t, r)
	}
	if _, err := tr.RoundTrip(testRequest(t)); !errors.Is(err, taskbudget.ErrExceeded) {
		t.Fatalf("budget denial must terminate summarizer retries: %v", err)
	}
	if physical.Load() != 2 || tr.CallCount() != 2 || tr.DeniedCount() != 1 {
		t.Fatalf("physical calls exceeded budget: %d %+v", physical.Load(), tr.Records())
	}
	got := tr.Records()[0]
	if got.RequestModel != "requested" || got.ResponseModel != "actual-v2" || got.Usage == nil || got.Usage.Input != 11 || got.Usage.Output != 5 || got.Usage.Cached != 2 || got.FinishReasons[0] != "stop" || got.PromptHash == "" || got.RequestHash == "" {
		t.Fatalf("missing wire evidence: %+v", got)
	}
	if !FinalCompletionsOK(tr.Records()) {
		t.Fatal("all final prompt attempts completed")
	}
}

func TestTransportRedactsErrorsBeforeSummarizerCanLogThem(t *testing.T) {
	for _, tc := range []struct {
		name string
		base roundTripFunc
	}{
		{"http error", func(*http.Request) (*http.Response, error) {
			r := wireResponse(`{"error":"secret-key and private endpoint"}`)
			r.StatusCode = 401
			return r, nil
		}},
		{"network error", func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial https://secret-key@private-endpoint: secret-key")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewBudgetTransport(tc.base, 1)
			r, err := tr.RoundTrip(testRequest(t))
			if err != nil && (strings.Contains(err.Error(), "secret-key") || strings.Contains(err.Error(), "private-endpoint")) {
				t.Fatal("raw network error escaped")
			}
			if r != nil && strings.Contains(consume(t, r), "secret-key") {
				t.Fatal("raw provider error escaped")
			}
			if FinalCompletionsOK(tr.Records()) {
				t.Fatal("failed calls must not count as completed")
			}
		})
	}
}

func TestTransportCapturesSSEAndRejectsMissingOrMalformedCompletionEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"complete", "data: {\"model\":\"reported\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":2}}\n\ndata: [DONE]\n", true},
		{"truncated", "data: {\"choices\":[{\"finish_reason\":\"length\"}]}\n", false},
		{"done without finish", "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n", false},
		{"malformed event", "data: invalid\n", false},
		{"missing JSON reason", `{"choices":[{"message":{"content":"looks complete"}}]}`, false},
		{"invalid usage", `{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":-1,"completion_tokens":2}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := NewBudgetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
				r := wireResponse(tc.body)
				if strings.HasPrefix(tc.body, "data:") {
					r.Header.Set("Content-Type", "text/event-stream")
				}
				return r, nil
			}), 1)
			r, err := tr.RoundTrip(testRequest(t))
			if err != nil {
				t.Fatal(err)
			}
			consume(t, r)
			if FinalCompletionsOK(tr.Records()) != tc.valid {
				t.Fatalf("wrong completion verdict %+v", tr.Records())
			}
			if tc.name == "invalid usage" && tr.Records()[0].Usage != nil {
				t.Fatal("negative usage must be unknown")
			}
		})
	}
}

func TestFinalCompletionAllowsRecoveredRetryButRetainsBothAttempts(t *testing.T) {
	records := []CallRecord{
		{PromptHash: "same", HTTPStatus: 200, FinishReasons: []string{"length"}},
		{PromptHash: "same", HTTPStatus: 200, FinishReasons: []string{"stop"}},
	}
	if !FinalCompletionsOK(records) {
		t.Fatal("a complete replacement may recover a truncated attempt")
	}
	records[1].PromptHash = "different"
	if FinalCompletionsOK(records) {
		t.Fatal("unrelated completion cannot repair an incomplete prompt")
	}
}

func TestTransportStopsReadingAnOversizedProviderBody(t *testing.T) {
	tr := NewBudgetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return wireResponse(strings.Repeat("x", captureLimit+100)), nil
	}), 1)
	r, err := tr.RoundTrip(testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err == nil || len(raw) > captureLimit+32768 || FinalCompletionsOK(tr.Records()) {
		t.Fatalf("oversized response must fail before unbounded decoding: bytes=%d err=%v", len(raw), err)
	}
}

func TestTransportFingerprintsLargeBoundedVisionRequests(t *testing.T) {
	tr := NewBudgetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return wireResponse(`{"choices":[{"finish_reason":"stop"}]}`), nil
	}), 1)
	payload := `{"model":"vision-model","messages":[{"role":"user","content":"` + strings.Repeat("x", captureLimit+1) + `"}]}`
	r, _ := http.NewRequest("POST", "https://provider.example/chat/completions", strings.NewReader(payload))
	resp, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	consume(t, resp)
	if got := tr.Records()[0]; got.RequestHash == "" || got.PromptHash == "" || !FinalCompletionsOK(tr.Records()) {
		t.Fatalf("large image payload lost version evidence: %+v", got)
	}
}
