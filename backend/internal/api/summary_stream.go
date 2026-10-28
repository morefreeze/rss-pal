package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Keep provider silence below the API proxy's 60-second idle read timeout.
const summaryHeartbeatInterval = 15 * time.Second

type summaryStreamWriter struct {
	writer          http.ResponseWriter
	flusher         http.Flusher
	mu              sync.Mutex
	cancelHeartbeat context.CancelFunc
	heartbeatDone   chan struct{}
}

func newSummaryStreamWriter(ctx context.Context, writer http.ResponseWriter, flusher http.Flusher, interval time.Duration) *summaryStreamWriter {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	s := &summaryStreamWriter{
		writer: writer, flusher: flusher,
		cancelHeartbeat: cancel, heartbeatDone: make(chan struct{}),
	}
	// Flush headers and a complete NDJSON frame before waiting for any model
	// output, including when the provider has not sent its response headers.
	s.writeAndFlush(map[string]any{"type": "heartbeat"})
	go func() {
		defer close(s.heartbeatDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if heartbeatCtx.Err() != nil {
					return
				}
				s.writeAndFlush(map[string]any{"type": "heartbeat"})
			}
		}
	}()
	return s
}

func (s *summaryStreamWriter) writeAndFlush(frame map[string]any) {
	data, err := json.Marshal(frame)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.writer.Write(append(data, '\n'))
	s.flusher.Flush()
}

// Join the heartbeat before Gin can reuse its writer. This deliberately leaves
// writeAndFlush usable for the transaction's later commit/rollback callbacks.
func (s *summaryStreamWriter) stopHeartbeat() {
	s.cancelHeartbeat()
	<-s.heartbeatDone
}
