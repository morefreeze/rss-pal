package summaryeval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/bytedance/rss-pal/internal/taskbudget"
)

const captureLimit = 8 << 20

// Four 8 MiB JPEGs expand to roughly 43 MiB of base64, plus text/prompts.
const requestCaptureLimit = 48 << 20

type callScope struct {
	kind, caseID string
	route        Route
}
type scopeKey struct{}

func withScope(ctx context.Context, kind, caseID string, route Route) context.Context {
	return context.WithValue(ctx, scopeKey{}, callScope{kind, caseID, route})
}

type Usage struct {
	Input  int64 `json:"input_tokens"`
	Output int64 `json:"output_tokens"`
	Cached int64 `json:"cached_input_tokens"`
}

// CallRecord intentionally excludes endpoints, headers, request bodies,
// response bodies, and provider error messages. Hashes permit retry inspection.
type CallRecord struct {
	Number        int      `json:"number"`
	Kind          string   `json:"kind"`
	CaseID        string   `json:"case_id"`
	Route         Route    `json:"route,omitempty"`
	RequestModel  string   `json:"requested_model"`
	ResponseModel string   `json:"reported_model,omitempty"`
	RequestHash   string   `json:"request_sha256"`
	PromptHash    string   `json:"messages_sha256"`
	HTTPStatus    int      `json:"http_status"`
	FinishReasons []string `json:"finish_reasons"`
	Usage         *Usage   `json:"usage,omitempty"`
	ErrorCode     string   `json:"error_code,omitempty"`
}

// BudgetTransport counts immediately before calling the underlying transport,
// so retries, control grading and output grading spend the same hard budget.
type BudgetTransport struct {
	base          http.RoundTripper
	limit         int
	mu            sync.Mutex
	calls, denied int
	records       []CallRecord
}

func NewBudgetTransport(base http.RoundTripper, limit int) *BudgetTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &BudgetTransport{base: base, limit: limit}
}

func (t *BudgetTransport) CallCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func (t *BudgetTransport) DeniedCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.denied
}

func (t *BudgetTransport) Records() []CallRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]CallRecord(nil), t.records...)
	for i := range out {
		out[i].FinishReasons = append([]string(nil), out[i].FinishReasons...)
		if out[i].Usage != nil {
			u := *out[i].Usage
			out[i].Usage = &u
		}
	}
	return out
}

func (t *BudgetTransport) update(index int, r CallRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records[index] = r
}

func (t *BudgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Context().Err() != nil {
		return nil, errors.New("evaluation context ended")
	}
	t.mu.Lock()
	if t.calls >= t.limit {
		t.denied++
		used := t.calls
		t.mu.Unlock()
		return nil, &taskbudget.LimitError{Reason: "eval_request_budget", Bucket: "ai", Used: used, Limit: t.limit}
	}
	scope, _ := req.Context().Value(scopeKey{}).(callScope)
	r := CallRecord{Number: t.calls + 1, Kind: scope.kind, CaseID: scope.caseID, Route: scope.route, ErrorCode: "request_metadata_unavailable"}
	index := len(t.records)
	t.records = append(t.records, r)
	t.calls++
	t.mu.Unlock()
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			b, readErr := io.ReadAll(io.LimitReader(body, requestCaptureLimit+1))
			body.Close()
			if readErr == nil && len(b) <= requestCaptureLimit {
				var in struct {
					Model    string          `json:"model"`
					Messages json.RawMessage `json:"messages"`
				}
				if json.Unmarshal(b, &in) == nil {
					r.RequestModel = safeWireModel(in.Model)
					r.RequestHash, r.PromptHash = hashText(string(b)), hashText(string(in.Messages))
					if r.RequestModel != "" && len(in.Messages) > 0 {
						r.ErrorCode = ""
					}
				}
			}
		}
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		r.ErrorCode = "provider_transport_error"
		t.update(index, r)
		return nil, errors.New("provider transport failed; raw error omitted")
	}
	r.HTTPStatus = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		r.ErrorCode = "provider_http_error"
		t.update(index, r)
		const safe = `{"error":"provider error body omitted"}`
		resp.Body = io.NopCloser(strings.NewReader(safe))
		resp.ContentLength = int64(len(safe))
		resp.Header = http.Header{"Content-Type": {"application/json"}}
		return resp, nil
	}
	t.update(index, r)
	resp.Body = &evidenceBody{ReadCloser: resp.Body, stream: strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream"), record: r,
		done: func(record CallRecord) { t.update(index, record) }}
	return resp, nil
}

func safeWireModel(s string) string {
	if validModel(s) {
		return s
	}
	return ""
}

type evidenceBody struct {
	io.ReadCloser
	stream   bool
	pending  []byte
	overflow bool
	readErr  bool
	once     sync.Once
	record   CallRecord
	done     func(CallRecord)
}

func (b *evidenceBody) Read(p []byte) (int, error) {
	if b.overflow {
		return 0, errors.New("provider response exceeds evaluation limit")
	}
	n, err := b.ReadCloser.Read(p)
	if !b.overflow {
		if len(b.pending)+n > captureLimit {
			allowed := captureLimit - len(b.pending)
			b.pending, b.overflow = nil, true
			return allowed, errors.New("provider response exceeds evaluation limit")
		} else {
			b.pending = append(b.pending, p[:n]...)
		}
	}
	if err != nil && err != io.EOF {
		b.readErr = true
		return n, errors.New("provider response read failed; raw error omitted")
	}
	return n, err
}

func (b *evidenceBody) parse(raw []byte) {
	var v struct {
		Model   string `json:"model"`
		Choices []struct {
			Finish string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			Input  *int64 `json:"prompt_tokens"`
			Output *int64 `json:"completion_tokens"`
			Detail struct {
				Cached int64 `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &v) != nil {
		b.record.ErrorCode = "invalid_completion_metadata"
		return
	}
	if m := safeWireModel(v.Model); m != "" {
		b.record.ResponseModel = m
	}
	for _, c := range v.Choices {
		if c.Finish != "" {
			switch c.Finish {
			case "stop", "length", "content_filter", "tool_calls", "function_call":
				b.record.FinishReasons = append(b.record.FinishReasons, c.Finish)
			default:
				b.record.FinishReasons = append(b.record.FinishReasons, "unrecognized")
			}
		}
	}
	if u := v.Usage; u != nil && u.Input != nil && u.Output != nil && *u.Input >= 0 && *u.Output >= 0 && u.Detail.Cached >= 0 && u.Detail.Cached <= *u.Input {
		b.record.Usage = &Usage{Input: *u.Input, Output: *u.Output, Cached: u.Detail.Cached}
	}
}

func (b *evidenceBody) Close() error {
	b.once.Do(func() {
		closeErr := b.ReadCloser.Close()
		if b.overflow || b.readErr || closeErr != nil {
			b.record.ErrorCode = "incomplete_completion_capture"
		} else if b.stream {
			for _, line := range bytes.Split(b.pending, []byte("\n")) {
				if bytes.HasPrefix(line, []byte("data:")) {
					data := bytes.TrimSpace(line[5:])
					if !bytes.Equal(data, []byte("[DONE]")) {
						b.parse(data)
					}
				}
			}
		} else {
			b.parse(b.pending)
		}
		b.pending = nil
		b.done(b.record)
	})
	return nil
}

// FinalCompletionsOK requires an explicit successful finish for the final
// attempt of each prompt. Earlier failures remain visible in the artifact.
func FinalCompletionsOK(records []CallRecord) bool {
	if len(records) == 0 {
		return false
	}
	last := map[string]CallRecord{}
	for _, r := range records {
		key := r.Kind + "\x00" + r.CaseID + "\x00" + string(r.Route) + "\x00" + r.RequestModel + "\x00" + r.PromptHash
		last[key] = r
	}
	for _, r := range last {
		if r.ErrorCode != "" || r.HTTPStatus != 200 || len(r.FinishReasons) == 0 {
			return false
		}
		for _, reason := range r.FinishReasons {
			if reason != "stop" {
				return false
			}
		}
	}
	return true
}
