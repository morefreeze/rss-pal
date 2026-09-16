package ai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/bytedance/rss-pal/internal/taskbudget"
)

func TestAIQuotaDenialNeverCallsOrRetriesUpstream(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	s := NewSummarizer("test", server.URL)
	var admissions int
	s.SetAdmission(func(context.Context) (func(), error) { admissions++; return nil, taskbudget.ErrExceeded })
	_, err := s.call(context.Background(), "test", 10)
	if !errors.Is(err, taskbudget.ErrExceeded) || calls.Load() != 0 || admissions != 1 {
		t.Fatalf("err=%v upstream=%d reservations=%d", err, calls.Load(), admissions)
	}
}
func TestAILeaseLivesUntilResponseClosed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("stream")) }))
	defer server.Close()
	s := NewSummarizer("test", server.URL)
	released := false
	s.SetAdmission(func(context.Context) (func(), error) { return func() { released = true }, nil })
	req, _ := http.NewRequest("GET", server.URL, nil)
	res, err := s.doAdmitted(req)
	if err != nil {
		t.Fatal(err)
	}
	if released {
		t.Fatal("stream lease released before response body consumed")
	}
	res.Body.Close()
	if !released {
		t.Fatal("stream lease leaked")
	}
}
