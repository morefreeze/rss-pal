package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type summaryConcurrencyRecorder struct {
	*httptest.ResponseRecorder
	active  atomic.Int32
	overlap atomic.Bool
}

func (w *summaryConcurrencyRecorder) enter() func() {
	if w.active.Add(1) != 1 {
		w.overlap.Store(true)
	}
	runtime.Gosched()
	return func() { w.active.Add(-1) }
}

func (w *summaryConcurrencyRecorder) Write(data []byte) (int, error) {
	defer w.enter()()
	return w.ResponseRecorder.Write(data)
}

func (w *summaryConcurrencyRecorder) Flush() {
	defer w.enter()()
	w.ResponseRecorder.Flush()
}

func TestSummaryStreamWriterSerializesNDJSON(t *testing.T) {
	w := &summaryConcurrencyRecorder{ResponseRecorder: httptest.NewRecorder()}
	stream := newSummaryStreamWriter(t.Context(), w, w, 100*time.Microsecond)
	defer stream.stopHeartbeat()
	var writers sync.WaitGroup
	for worker := range 8 {
		writers.Go(func() {
			for part := range 64 {
				stream.writeAndFlush(map[string]any{"type": "brief_delta", "text": fmt.Sprintf("%d/%d 中文\n\"", worker, part)})
			}
		})
	}
	writers.Wait()
	stream.stopHeartbeat()
	// Commit callbacks retain a usable writer after the heartbeat is joined.
	stream.writeAndFlush(map[string]any{"type": "done"})
	if w.overlap.Load() {
		t.Fatal("heartbeat and model frames used ResponseWriter concurrently")
	}
	scanner := bufio.NewScanner(strings.NewReader(w.Body.String()))
	deltas, heartbeats, done := 0, 0, false
	for scanner.Scan() {
		var frame summaryHTTPFrame
		if err := json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			t.Fatalf("interleaved NDJSON %q: %v", scanner.Text(), err)
		}
		if done {
			t.Fatalf("frame after terminal done: %+v", frame)
		}
		switch frame.Type {
		case "brief_delta":
			deltas++
		case "heartbeat":
			heartbeats++
		case "done":
			done = true
		default:
			t.Fatalf("unexpected frame %+v", frame)
		}
	}
	if err := scanner.Err(); err != nil || deltas != 8*64 || heartbeats < 1 || !done {
		t.Fatalf("deltas=%d heartbeats=%d done=%v err=%v", deltas, heartbeats, done, err)
	}
}

type summaryBlockedHeartbeatWriter struct {
	*httptest.ResponseRecorder
	writes  int
	entered chan struct{}
	release chan struct{}
}

func (w *summaryBlockedHeartbeatWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes == 2 {
		close(w.entered)
		<-w.release
	}
	return w.ResponseRecorder.Write(data)
}

func TestSummaryStreamWriterStopWaitsForInFlightHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &summaryBlockedHeartbeatWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
	stream := newSummaryStreamWriter(ctx, w, w, time.Millisecond)
	var releaseOnce sync.Once
	defer stream.stopHeartbeat()
	defer releaseOnce.Do(func() { close(w.release) })
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("periodic heartbeat did not reach writer")
	}
	cancel()
	stopped := make(chan struct{})
	go func() { stream.stopHeartbeat(); close(stopped) }()
	select {
	case <-stopped:
		t.Fatal("heartbeat stop returned while a write was still in flight")
	case <-time.After(20 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(w.release) })
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("heartbeat did not stop after canceled write completed")
	}
	stream.writeAndFlush(map[string]any{"type": "done"})
	if got := w.Body.String(); !strings.HasSuffix(got, "{\"type\":\"done\"}\n") {
		t.Fatalf("completion callback after cancellation/stop could not write: %q", got)
	}
}

func TestSummaryStreamWriterCancellationStopsHeartbeat(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	w := httptest.NewRecorder()
	stream := newSummaryStreamWriter(ctx, w, w, time.Hour)
	defer stream.stopHeartbeat()
	cancel()
	select {
	case <-stream.heartbeatDone:
	case <-time.After(time.Second):
		t.Fatal("heartbeat goroutine outlived canceled context")
	}
	if got := w.Body.String(); got != "{\"type\":\"heartbeat\"}\n" {
		t.Fatalf("unexpected canceled stream: %q", got)
	}
}
