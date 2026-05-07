package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
