package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRedactedAccessLoggerUsesRouteTemplate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	router := gin.New()
	router.Use(RedactedAccessLogger(&logs))
	router.GET("/api/s/:short_code", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/s/Aa0000000000?secret=query", nil)
	request.RemoteAddr = "203.0.113.7:4321"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	got := logs.String()
	for _, want := range []string{
		"timestamp=",
		"status=204",
		"latency=",
		"client_ip=203.0.113.7",
		"method=GET",
		"path=/api/s/:short_code",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q does not contain %q", got, want)
		}
	}
	for _, secret := range []string{"Aa0000000000", "secret", "query"} {
		if strings.Contains(got, secret) {
			t.Errorf("log %q contains secret %q", got, secret)
		}
	}
}

func TestRedactedAccessLoggerHidesUnmatchedPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	router := gin.New()
	router.Use(RedactedAccessLogger(&logs))

	request := httptest.NewRequest(http.MethodGet, "/private-value", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	got := logs.String()
	if !strings.Contains(got, "path=<unmatched>") {
		t.Fatalf("log %q does not contain unmatched marker", got)
	}
	if strings.Contains(got, "/private-value") {
		t.Fatalf("log %q contains unmatched request path", got)
	}
}

func TestRedactedRecoveryDoesNotLogRecoveredValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var logs bytes.Buffer
	router := gin.New()
	router.Use(RedactedRecovery(&logs))
	router.GET("/panic", func(c *gin.Context) {
		panic("Aa0000000000")
	})

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	got := logs.String()
	if !strings.Contains(got, "panic recovered") {
		t.Fatalf("log %q does not contain recovery marker", got)
	}
	if !strings.Contains(got, "runtime/debug.Stack") {
		t.Fatalf("log %q does not contain a stack trace", got)
	}
	if strings.Contains(got, "Aa0000000000") {
		t.Fatalf("log %q contains recovered value", got)
	}
}
