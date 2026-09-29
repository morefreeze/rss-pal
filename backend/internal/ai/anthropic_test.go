package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bytedance/rss-pal/internal/aiusage"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnthropicJSONAndImageTranslation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "secret" || r.Header.Get("Authorization") != "" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("wrong native path or headers")
		}
		var b map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Error(err)
		}
		if b["model"] != "claude-sonnet-4-6" || b["response_format"] != nil || b["stream_options"] != nil {
			t.Errorf("request=%v", b)
		}
		if !strings.Contains(fmt.Sprint(b["system"]), "system prompt") {
			t.Error("system missing")
		}
		ms := b["messages"].([]interface{})
		bs := ms[0].(map[string]interface{})["content"].([]interface{})
		src := bs[1].(map[string]interface{})["source"].(map[string]interface{})
		if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "aGVsbG8=" {
			t.Errorf("source=%v", src)
		}
		fmt.Fprint(w, `{"type":"message","model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello"}],"usage":{"input_tokens":20,"cache_creation_input_tokens":10,"cache_read_input_tokens":30,"output_tokens":7}}`)
	}))
	defer server.Close()
	s := NewSummarizer("secret", server.URL+"/v1")
	s.providerProtocol = "anthropic"
	var rec aiusage.Record
	s.usageRecorder = func(r aiusage.Record) { rec = r }
	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"claude-sonnet-4-6","max_tokens":100,"response_format":{"type":"json_object"},"messages":[{"role":"system","content":"system prompt"},{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGVsbG8="}}]}]}`))
	req.Header.Set("Authorization", "Bearer secret")
	res, err := s.doAdmitted(req)
	if err != nil {
		t.Fatal(err)
	}
	var out chatResponse
	err = json.NewDecoder(res.Body).Decode(&out)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Choices) != 1 || out.Choices[0].Message.Content != "hello" {
		t.Fatalf("out=%+v", out)
	}
	if !rec.Known || rec.Input != 60 || rec.Cached != 30 || rec.Output != 7 || rec.Model != "claude-sonnet-4-6" {
		t.Fatalf("usage=%+v", rec)
	}
}
func TestAnthropicStreamingCompletionAndFailures(t *testing.T) {
	start := `data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":20,"cache_creation_input_tokens":10,"cache_read_input_tokens":30,"output_tokens":1}}}` + "\n\n"
	txt := `data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}` + "\n\n"
	delta := `data: {"type":"message_delta","usage":{"output_tokens":7}}` + "\n\n"
	stop := `data: {"type":"message_stop"}` + "\n\n"
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{{"complete", start + txt + delta + delta + stop, true}, {"truncated", start + txt + delta, false}, {"error", start + txt + `data: {"type":"error","error":{"message":"secret"}}` + "\n\n", false}, {"malformed", start + "data: {broken}\n\n", false}, {"missing start", txt + delta + stop, false}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			s := NewSummarizer("secret", server.URL)
			s.providerProtocol = "anthropic"
			var rec aiusage.Record
			s.usageRecorder = func(r aiusage.Record) { rec = r }
			releases := 0
			s.SetAdmission(func(context.Context) (func(), error) { return func() { releases++ }, nil })
			result, err := s.callStream(context.Background(), "hello", 100, func(string) {})
			if (err == nil) != tc.ok {
				t.Fatalf("result=%q err=%v", result, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("secret leaked")
			}
			if tc.ok && (result != "hello" || !rec.Known || rec.Input != 60 || rec.Cached != 30 || rec.Output != 7) {
				t.Fatalf("result=%q usage=%+v", result, rec)
			}
			if !tc.ok && rec.Known {
				t.Fatalf("incomplete known: %+v", rec)
			}
			if releases != 1 {
				t.Fatalf("releases=%d", releases)
			}
		})
	}
}
func TestAnthropicHTTPErrorSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"secret"}}`)
	}))
	defer server.Close()
	s := NewSummarizer("secret", server.URL)
	s.providerProtocol = "anthropic"
	req, _ := http.NewRequest("POST", server.URL+"/chat/completions", strings.NewReader(`{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"x"}]}`))
	res, err := s.doAdmitted(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 401 || strings.Contains(string(b), "secret") {
		t.Fatalf("status=%d body=%s", res.StatusCode, b)
	}
}

func TestAnthropicJSONMalformedAndMissingUsage(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"malformed", `{"type":"message"`, false},
		{"wrong type", `{"type":"error","error":{"message":"secret"}}`, false},
		{"no usage", `{"type":"message","content":[{"type":"text","text":"ok"}]}`, true},
		{"partial usage", `{"type":"message","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":20}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			s := NewSummarizer("secret", server.URL)
			s.providerProtocol = "anthropic"
			var rec aiusage.Record
			records, releases := 0, 0
			s.usageRecorder = func(r aiusage.Record) { rec = r; records++ }
			s.SetAdmission(func(context.Context) (func(), error) { return func() { releases++ }, nil })
			req, _ := http.NewRequest("POST", server.URL+"/chat/completions", strings.NewReader(`{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"x"}]}`))
			res, err := s.doAdmitted(req)
			if (err == nil) != tc.ok {
				t.Fatalf("err=%v", err)
			}
			if res != nil {
				io.ReadAll(res.Body)
				res.Body.Close()
			}
			if rec.Known || records != 1 || releases != 1 {
				t.Fatalf("usage=%+v records=%d releases=%d", rec, records, releases)
			}
		})
	}
}

func TestAnthropicStreamEarlyCloseClosesUpstream(t *testing.T) {
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":20,\"output_tokens\":1}}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	s := NewSummarizer("secret", server.URL)
	s.providerProtocol = "anthropic"
	records, releases := 0, 0
	s.usageRecorder = func(r aiusage.Record) {
		records++
		if r.Known {
			t.Error("early close known")
		}
	}
	s.SetAdmission(func(context.Context) (func(), error) { return func() { releases++ }, nil })
	req, _ := http.NewRequest("POST", server.URL+"/chat/completions", strings.NewReader(`{"model":"claude","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"x"}]}`))
	res, err := s.doAdmitted(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("upstream not canceled")
	}
	if records != 1 || releases != 1 {
		t.Fatalf("records=%d releases=%d", records, releases)
	}
}

func TestAnthropicDoesNotForwardKeyOnRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; fmt.Fprint(w, `{"type":"message"}`) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	s := NewSummarizer("secret", server.URL)
	s.providerProtocol = "anthropic"
	req, _ := http.NewRequest("POST", server.URL+"/chat/completions", strings.NewReader(`{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"x"}]}`))
	res, err := s.doAdmitted(req)
	if res != nil {
		res.Body.Close()
	}
	if reached {
		t.Fatal("forwarded native API key on redirect")
	}
	if err == nil && res.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("redirect not reported: %v", res.StatusCode)
	}
}

func TestAnthropicStreamMergesCumulativeUsageFields(t *testing.T) {
	for _, tc := range []struct {
		name, usage   string
		input, cached int64
	}{
		{"final input replaces initial", `{"input_tokens":10682,"output_tokens":9}`, 10732, 30},
		{"absent fields retain initial", `{"output_tokens":9}`, 2729, 30},
		{"all cumulative counters replace initial", `{"input_tokens":10682,"cache_creation_input_tokens":40,"cache_read_input_tokens":60,"output_tokens":9}`, 10782, 60},
		{"explicit zero resets cache", `{"input_tokens":10682,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":9}`, 10682, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":2679,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":1}}}` + "\n\n" +
				`data: {"type":"message_delta","usage":` + tc.usage + "}\n\n" +
				`data: {"type":"message_delta","usage":{"output_tokens":12}}` + "\n\n" +
				`data: {"type":"message_stop"}` + "\n\n"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			s := NewSummarizer("secret", server.URL)
			s.providerProtocol = "anthropic"
			var rec aiusage.Record
			s.usageRecorder = func(r aiusage.Record) { rec = r }
			_, err := s.callStream(context.Background(), "hello", 100, func(string) {})
			if err != nil {
				t.Fatal(err)
			}
			if !rec.Known || rec.Input != tc.input || rec.Cached != tc.cached || rec.Output != 12 {
				t.Fatalf("usage=%+v; want input=%d cached=%d output=12", rec, tc.input, tc.cached)
			}
		})
	}
}

type anthropicTestTransport func(*http.Request) (*http.Response, error)

func (f anthropicTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type anthropicFailingBody struct{ err error }

func (b anthropicFailingBody) Read([]byte) (int, error) { return 0, b.err }
func (b anthropicFailingBody) Close() error             { return nil }
func TestAnthropicSafeErrorsPreserveRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		read      bool
		cause     error
		transient bool
	}{
		{"transport EOF", false, fmt.Errorf("secret transport: %w", io.EOF), true},
		{"read unexpected EOF", true, fmt.Errorf("secret response: %w", io.ErrUnexpectedEOF), true},
		{"transport reset", false, errors.New("secret connection reset by peer"), true},
		{"permanent transport", false, errors.New("secret invalid TLS certificate"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewSummarizer("secret", "https://example.test/v1")
			s.providerProtocol = "anthropic"
			s.httpClient = &http.Client{Transport: anthropicTestTransport(func(r *http.Request) (*http.Response, error) {
				if !tc.read {
					return nil, tc.cause
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: anthropicFailingBody{tc.cause}}, nil
			})}
			req, _ := http.NewRequest("POST", "https://example.test/v1/chat/completions", strings.NewReader(`{"model":"claude","max_tokens":10,"messages":[{"role":"user","content":"x"}]}`))
			_, err := s.doAdmitted(req)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatalf("leaked error: %v", err)
			}
			if got := isTransientNetErr(fmt.Errorf("vision request: %w", err)); got != tc.transient {
				t.Fatalf("isTransientNetErr=%v, want=%v error=%v", got, tc.transient, err)
			}
		})
	}
}
