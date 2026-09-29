package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type compatibleTransport func(*http.Request) (*http.Response, error)

func (f compatibleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOfficialOpenAIReasoningTokenParameter(t *testing.T) {
	s := NewSummarizerWithModel("key", "https://api.openai.com/v1", "gpt-5-mini")
	s.articleRouted = true
	s.httpClient = &http.Client{Transport: compatibleTransport(func(r *http.Request) (*http.Response, error) {
		var p map[string]any
		if e := json.NewDecoder(r.Body).Decode(&p); e != nil {
			t.Fatal(e)
		}
		if p["max_tokens"] != nil || p["max_completion_tokens"] != float64(100) {
			t.Fatal(p)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`))}, nil
	})}
	got, e := s.call(context.Background(), "prompt", 100)
	if e != nil || got != "ok" {
		t.Fatal(got, e)
	}
}
