package api

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type summaryHTTPFrame struct {
	Type string `json:"type"`
	Text string `json:"text"`
	err  error
}

func readSummaryHTTPFrames(response *http.Response) <-chan summaryHTTPFrame {
	frames := make(chan summaryHTTPFrame, 256)
	go func() {
		defer close(frames)
		var reader io.Reader = response.Body
		if response.Header.Get("Content-Encoding") == "gzip" {
			compressed, err := gzip.NewReader(response.Body)
			if err != nil {
				frames <- summaryHTTPFrame{err: err}
				return
			}
			defer compressed.Close()
			reader = compressed
		}
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			var frame summaryHTTPFrame
			if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
				frames <- summaryHTTPFrame{err: fmt.Errorf("invalid NDJSON %q: %w", scanner.Text(), err)}
				return
			}
			frames <- frame
		}
		if err := scanner.Err(); err != nil {
			frames <- summaryHTTPFrame{err: err}
		}
	}()
	return frames
}

func nextSummaryHTTPFrame(t *testing.T, frames <-chan summaryHTTPFrame, timeout time.Duration) summaryHTTPFrame {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("summary stream closed before expected frame")
		}
		if frame.err != nil {
			t.Fatal(frame.err)
		}
		return frame
	case <-time.After(timeout):
		t.Fatal("summary stream was idle while provider was waiting")
		return summaryHTTPFrame{}
	}
}

func openSummaryHTTPStream(t *testing.T, ctx context.Context, server *httptest.Server, path string, providerEntered <-chan struct{}) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	type result struct {
		response *http.Response
		err      error
	}
	results := make(chan result, 1)
	go func() {
		response, err := server.Client().Do(request)
		results <- result{response, err}
	}()
	select {
	case <-providerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not called")
	}
	select {
	case result := <-results:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.response.StatusCode != http.StatusOK || result.response.Header.Get("Content-Type") != "application/x-ndjson" || result.response.Header.Get("X-Accel-Buffering") != "no" {
			t.Fatalf("unexpected stream headers: %d %v", result.response.StatusCode, result.response.Header)
		}
		t.Cleanup(func() { result.response.Body.Close() })
		return result.response
	case <-time.After(time.Second):
		t.Fatal("summary headers were not flushed before the provider's first token")
		return nil
	}
}

func TestSummaryStreamKeepsSilentProviderAliveUntilCommit(t *testing.T) {
	briefEntered, detailedEntered := make(chan struct{}), make(chan struct{})
	briefRelease, detailedRelease := make(chan struct{}), make(chan struct{})
	var briefOnce, detailedOnce sync.Once
	defer briefOnce.Do(func() { close(briefRelease) })
	defer detailedOnce.Do(func() { close(detailedRelease) })
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		phase := calls.Add(1)
		entered, release, text := briefEntered, briefRelease, "b"
		if phase == 2 {
			entered, release, text = detailedEntered, detailedRelease, "d"
		} else if phase != 1 {
			t.Errorf("unexpected provider call %d", phase)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		for range 32 {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", text)
			w.(http.Flusher).Flush()
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer provider.Close()
	readyToCommit, allowCommit := make(chan struct{}), make(chan struct{})
	var commitOnce sync.Once
	router, db, id := summaryTestRouterWithMiddleware(t, provider.URL, false, func(c *gin.Context) {
		c.Next()
		close(readyToCommit)
		<-allowCommit
	})
	server := httptest.NewServer(summaryGzipRouter(router))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defer commitOnce.Do(func() { close(allowCommit) })
	response := openSummaryHTTPStream(t, ctx, server, fmt.Sprintf("/api/articles/%d/summary?stream=1", id), briefEntered)
	frames := readSummaryHTTPFrames(response)
	if frame := nextSummaryHTTPFrame(t, frames, time.Second); frame.Type != "heartbeat" || frame.Text != "" {
		t.Fatalf("first frame before any provider token = %+v, want heartbeat", frame)
	}
	// This checks the production interval, including silence after the provider
	// has flushed its own headers but has not emitted a model token.
	if frame := nextSummaryHTTPFrame(t, frames, 18*time.Second); frame.Type != "heartbeat" {
		t.Fatalf("silent brief phase frame = %+v, want heartbeat", frame)
	}
	briefOnce.Do(func() { close(briefRelease) })
	var brief strings.Builder
	for brief.Len() < 32 {
		frame := nextSummaryHTTPFrame(t, frames, 5*time.Second)
		switch frame.Type {
		case "brief_delta":
			brief.WriteString(frame.Text)
		case "heartbeat":
		default:
			t.Fatalf("unexpected brief phase frame: %+v", frame)
		}
	}
	select {
	case <-detailedEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("detailed provider was not called")
	}
	if frame := nextSummaryHTTPFrame(t, frames, 18*time.Second); frame.Type != "heartbeat" {
		t.Fatalf("silent detailed phase frame = %+v, want heartbeat", frame)
	}
	detailedOnce.Do(func() { close(detailedRelease) })
	if frame := nextSummaryHTTPFrame(t, frames, 5*time.Second); frame.Type != "brief_phase_done" {
		t.Fatalf("detailed phase transition = %+v", frame)
	}
	var detailed strings.Builder
	for detailed.Len() < 32 {
		frame := nextSummaryHTTPFrame(t, frames, 5*time.Second)
		switch frame.Type {
		case "detailed_delta":
			detailed.WriteString(frame.Text)
		case "heartbeat":
		default:
			t.Fatalf("unexpected detailed phase frame: %+v", frame)
		}
	}
	select {
	case <-readyToCommit:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish before commit")
	}
	var savedBrief string
	if err := db.QueryRow(`SELECT summary_brief FROM articles WHERE id=$1`, id).Scan(&savedBrief); err != nil || savedBrief != "prior brief" {
		t.Fatalf("before commit summary=%q err=%v", savedBrief, err)
	}
	select {
	case frame := <-frames:
		t.Fatalf("frame emitted while transaction was still uncommitted: %+v", frame)
	case <-time.After(100 * time.Millisecond):
	}
	commitOnce.Do(func() { close(allowCommit) })
	for _, want := range []string{"brief_done", "detailed_done", "done"} {
		if frame := nextSummaryHTTPFrame(t, frames, 5*time.Second); frame.Type != want {
			t.Fatalf("completion frame = %+v, want %s", frame, want)
		}
	}
	select {
	case frame, ok := <-frames:
		if ok {
			t.Fatalf("frame after done: %+v", frame)
		}
	case <-time.After(time.Second):
		t.Fatal("summary stream did not close after done")
	}
	var savedDetailed, state string
	if err := db.QueryRow(`SELECT a.summary_brief,a.summary_detailed,j.state FROM articles a JOIN article_summary_jobs j ON j.article_id=a.id WHERE a.id=$1`, id).Scan(&savedBrief, &savedDetailed, &state); err != nil || savedBrief != brief.String() || savedDetailed != detailed.String() || state != "done" || calls.Load() != 2 {
		t.Fatalf("committed brief=%q detailed=%q state=%q calls=%d err=%v", savedBrief, savedDetailed, state, calls.Load(), err)
	}
}

func TestSummaryStreamCancellationPreservesPriorSummary(t *testing.T) {
	entered, providerCanceled, handlerDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(providerCanceled)
	}))
	defer provider.Close()
	router, db, id := summaryTestRouter(t, provider.URL, false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		router.ServeHTTP(w, r)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	response := openSummaryHTTPStream(t, ctx, server, fmt.Sprintf("/articles/%d/summary?stream=1", id), entered)
	if frame := nextSummaryHTTPFrame(t, readSummaryHTTPFrames(response), time.Second); frame.Type != "heartbeat" {
		t.Fatalf("first frame = %+v, want heartbeat", frame)
	}
	cancel()
	for _, stopped := range []<-chan struct{}{providerCanceled, handlerDone} {
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("request cancellation did not stop provider and handler")
		}
	}
	var brief, detailed, state string
	if err := db.QueryRow(`SELECT a.summary_brief,a.summary_detailed,j.state FROM articles a JOIN article_summary_jobs j ON j.article_id=a.id WHERE a.id=$1`, id).Scan(&brief, &detailed, &state); err != nil || brief != "prior brief" || detailed != "prior detailed" || state != "failed" || calls.Load() != 1 {
		t.Fatalf("canceled brief=%q detailed=%q state=%q calls=%d err=%v", brief, detailed, state, calls.Load(), err)
	}
}

func TestSummaryNonStreamThroughCompressionKeepsJSONResult(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"valid summary"},"finish_reason":"stop"}]}`)
	}))
	defer provider.Close()
	router, db, id := summaryTestRouter(t, provider.URL, false)
	server := httptest.NewServer(summaryGzipRouter(router))
	defer server.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, fmt.Sprintf("%s/api/articles/%d/summary", server.URL, id), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept-Encoding", "gzip")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("nonstream response: %d %v", response.StatusCode, response.Header)
	}
	var result struct {
		Brief    string `json:"summary_brief"`
		Detailed string `json:"summary_detailed"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || result.Brief != "valid summary" || result.Detailed != "valid summary" || calls.Load() != 2 {
		t.Fatalf("nonstream result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM article_summary_jobs WHERE article_id=$1`, id).Scan(&state); err != nil || state != "done" {
		t.Fatalf("nonstream persisted job state=%q err=%v", state, err)
	}
}

// Apply the exact production compression middleware to the public route. The
// existing fixture keeps its short internal route and real RLS transaction.
func summaryGzipRouter(inner http.Handler) *gin.Engine {
	router := gin.New()
	router.Use(ResponseCompression())
	router.POST("/api/articles/:id/summary", func(c *gin.Context) {
		c.Request.URL.Path = strings.TrimPrefix(c.Request.URL.Path, "/api")
		inner.ServeHTTP(c.Writer, c.Request)
	})
	return router
}
