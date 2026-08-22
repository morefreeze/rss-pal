package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type heartbeatReaderStub struct {
	lastSeen time.Time
	err      error
}

func (s heartbeatReaderStub) LastSeen(string) (time.Time, error) {
	return s.lastSeen, s.err
}

func TestWorkerHealthReportsOKAtThreeMinuteThreshold(t *testing.T) {
	now := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	handler := NewSystemHealthHandler(heartbeatReaderStub{lastSeen: now.Add(-3 * time.Minute)}, func() time.Time { return now })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/internal/health/worker", nil)
	handler.Worker(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); body != `{"status":"ok"}` {
		t.Fatalf("body = %s, want safe ok response", body)
	}
}

func TestWorkerHealthReportsDownAfterThreeMinuteThreshold(t *testing.T) {
	now := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	handler := NewSystemHealthHandler(heartbeatReaderStub{lastSeen: now.Add(-3*time.Minute - time.Second)}, func() time.Time { return now })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/internal/health/worker", nil)
	handler.Worker(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if body := recorder.Body.String(); body != `{"status":"down"}` {
		t.Fatalf("body = %s, want safe down response", body)
	}
}

func TestWorkerHealthDoesNotExposeHeartbeatReadErrors(t *testing.T) {
	now := time.Date(2026, time.August, 22, 10, 0, 0, 0, time.UTC)
	handler := NewSystemHealthHandler(heartbeatReaderStub{err: errors.New("database password leaked")}, func() time.Time { return now })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/internal/health/worker", nil)
	handler.Worker(ctx)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if body := recorder.Body.String(); body != `{"status":"down"}` {
		t.Fatalf("body = %s, want safe down response", body)
	}
	if body := recorder.Body.String(); body == "" || strings.Contains(body, "database password leaked") {
		t.Fatalf("body exposes repository error: %s", body)
	}
}
