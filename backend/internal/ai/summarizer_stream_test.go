package ai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type streamCaptured struct {
	bodies [][]byte
}

func newStreamCaptureServer(t *testing.T) (*httptest.Server, *streamCaptured) {
	t.Helper()
	cap := &streamCaptured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.bodies = append(cap.bodies, body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

func TestCallStream_AccumulatesDeltasAndReturnsFullText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"content":"Hello"}}]}`,
			`{"choices":[{"delta":{"content":", "}}]}`,
			`{"choices":[{"delta":{"content":"world"}}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")
	var got []string
	full, err := s.callStream(context.Background(), "prompt", 100, func(delta string) {
		got = append(got, delta)
	})
	if err != nil {
		t.Fatalf("callStream returned error: %v", err)
	}
	if full != "Hello, world" {
		t.Errorf("full = %q, want %q", full, "Hello, world")
	}
	if strings.Join(got, "") != "Hello, world" {
		t.Errorf("deltas joined = %q, want %q", strings.Join(got, ""), "Hello, world")
	}
}

func TestCallStream_HandlesEmptyDeltaChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`{"choices":[{"delta":{"role":"assistant"}}]}`,
			`{"choices":[{"delta":{"content":"OK"}}]}`,
			`{"choices":[{"delta":{}}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("k", srv.URL, "m")
	var deltas []string
	full, err := s.callStream(context.Background(), "p", 100, func(d string) {
		deltas = append(deltas, d)
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if full != "OK" {
		t.Errorf("full = %q, want %q", full, "OK")
	}
	if len(deltas) != 1 || deltas[0] != "OK" {
		t.Errorf("deltas = %v, want [\"OK\"]", deltas)
	}
}

func TestCallStream_ReturnsErrorOnNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "boom")
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("k", srv.URL, "m")
	_, err := s.callStream(context.Background(), "p", 100, func(string) {})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not mention status 500", err.Error())
	}
}

func TestStreamDetailedPromptsUseAnchorsButBriefPromptsDoNot(t *testing.T) {
	srv, cap := newStreamCaptureServer(t)
	s := NewSummarizerWithModel("test-key", srv.URL, "test-model")

	if _, err := s.SummarizeStream(context.Background(), "Title", articleAnchorTestContent, func(string) {}, func(string) {}); err != nil {
		t.Fatalf("SummarizeStream: %v", err)
	}
	if _, err := s.SummarizeWithTemplateStream(
		context.Background(), "Title", articleAnchorTestContent,
		"STREAM BRIEF TEMPLATE\n{title}\n{content}",
		"STREAM DETAILED TEMPLATE\n{title}\n{content}",
		func(string) {}, func(string) {},
	); err != nil {
		t.Fatalf("SummarizeWithTemplateStream: %v", err)
	}
	if len(cap.bodies) != 4 {
		t.Fatalf("captured %d requests, want 4", len(cap.bodies))
	}

	assertTextPromptWithoutArticleAnchors(t, requestUserText(t, cap.bodies[0]), "stream brief")
	assertTextPromptWithArticleAnchors(t, requestUserText(t, cap.bodies[1]), "stream detailed")
	assertTextPromptWithoutArticleAnchors(t, requestUserText(t, cap.bodies[2]), "stream template brief")
	detailedTemplate := requestUserText(t, cap.bodies[3])
	assertTextPromptWithArticleAnchors(t, detailedTemplate, "stream template detailed")
	if !strings.Contains(detailedTemplate, "STREAM DETAILED TEMPLATE") ||
		!strings.HasSuffix(detailedTemplate, detailedArticleAnchorInstruction) {
		t.Errorf("custom stream detailed template should precede system-owned instruction:\n%s", detailedTemplate)
	}
}
