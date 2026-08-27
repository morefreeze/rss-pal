package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestZAIReaderLifecycleAndCacheMode(t *testing.T) {
	for _, tt := range []struct {
		name    string
		noCache bool
	}{
		{name: "cached", noCache: false},
		{name: "fresh", noCache: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var methods []string
			var callArguments map[string]any

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.Header.Get("Authorization"); got != "Bearer reader-test-key" {
					t.Errorf("Authorization = %q", got)
				}
				if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
					t.Errorf("Accept = %q", got)
				}

				var request struct {
					JSONRPC string         `json:"jsonrpc"`
					ID      any            `json:"id"`
					Method  string         `json:"method"`
					Params  map[string]any `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				mu.Lock()
				methods = append(methods, request.Method)
				mu.Unlock()

				switch request.Method {
				case "initialize":
					if got := request.Params["protocolVersion"]; got != "2024-11-05" {
						t.Errorf("protocolVersion = %#v", got)
					}
					w.Header().Set("Mcp-Session-Id", "reader-session")
					writeSSE(t, w, map[string]any{
						"jsonrpc": "2.0",
						"id":      request.ID,
						"result": map[string]any{
							"protocolVersion": "2024-11-05",
							"capabilities":    map[string]any{},
							"serverInfo":      map[string]any{"name": "reader-test", "version": "1"},
						},
					})
				case "notifications/initialized":
					if got := r.Header.Get("Mcp-Session-Id"); got != "reader-session" {
						t.Errorf("notification session = %q", got)
					}
					w.WriteHeader(http.StatusAccepted)
				case "tools/call":
					if got := r.Header.Get("Mcp-Session-Id"); got != "reader-session" {
						t.Errorf("tool session = %q", got)
					}
					if got := request.Params["name"]; got != "webReader" {
						t.Errorf("tool name = %#v", got)
					}
					callArguments, _ = request.Params["arguments"].(map[string]any)
					readerResult, _ := json.Marshal(map[string]any{
						"title":   "Example title",
						"url":     "https://example.com/article",
						"content": "# Reader markdown\n\nUseful content.",
					})
					writeSSE(t, w, map[string]any{
						"jsonrpc": "2.0",
						"id":      request.ID,
						"result": map[string]any{
							"content": []map[string]any{{"type": "text", "text": string(readerResult)}},
						},
					})
				default:
					t.Errorf("unexpected method %q", request.Method)
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer server.Close()

			reader := newZAIReader("reader-test-key", server.URL, server.Client())
			result, err := reader.fetch(context.Background(), "https://example.com/article", tt.noCache)
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if result.Title != "Example title" || result.Content != "# Reader markdown\n\nUseful content." {
				t.Fatalf("result = %#v", result)
			}
			mu.Lock()
			gotMethods := append([]string(nil), methods...)
			mu.Unlock()
			wantMethods := []string{"initialize", "notifications/initialized", "tools/call"}
			if fmt.Sprint(gotMethods) != fmt.Sprint(wantMethods) {
				t.Fatalf("methods = %v, want %v", gotMethods, wantMethods)
			}
			if callArguments == nil {
				t.Fatal("tool arguments were not recorded")
			}
			assertJSONArgument(t, callArguments, "url", "https://example.com/article")
			assertJSONArgument(t, callArguments, "timeout", float64(20))
			assertJSONArgument(t, callArguments, "no_cache", tt.noCache)
			assertJSONArgument(t, callArguments, "return_format", "markdown")
			assertJSONArgument(t, callArguments, "retain_images", true)
			assertJSONArgument(t, callArguments, "no_gfm", false)
			assertJSONArgument(t, callArguments, "keep_img_data_url", false)
			assertJSONArgument(t, callArguments, "with_images_summary", false)
			assertJSONArgument(t, callArguments, "with_links_summary", false)
		})
	}
}

func TestZAIReaderFailureClassesAreSafe(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "non 2xx",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("VERY_SECRET_BODY"))
			},
		},
		{
			name: "missing session id",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
			},
		},
		{
			name: "json rpc error",
			handler: sessionHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, map[string]any{"jsonrpc": "2.0", "id": 2, "error": map[string]any{"code": -32000, "message": "VERY_SECRET_BODY"}})
			}),
		},
		{
			name: "tool is error",
			handler: sessionHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, toolResult(true, `{"error":"VERY_SECRET_BODY"}`))
			}),
		},
		{
			name: "missing text block",
			handler: sessionHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]any{"content": []map[string]any{{"type": "image"}}}})
			}),
		},
		{
			name: "malformed nested json",
			handler: sessionHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, toolResult(false, "VERY_SECRET_BODY"))
			}),
		},
		{
			name: "embedded reader error",
			handler: sessionHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, toolResult(false, `{"error":"VERY_SECRET_BODY"}`))
			}),
		},
		{
			name: "empty content",
			handler: sessionHandler(t, func(w http.ResponseWriter, _ *http.Request) {
				writeSSE(t, w, toolResult(false, `{"title":"empty","content":"   "}`))
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			reader := newZAIReader("secret-reader-token", server.URL, server.Client())
			_, err := reader.fetch(context.Background(), "https://example.com", false)
			if err == nil {
				t.Fatal("expected error")
			}
			for _, secret := range []string{"secret-reader-token", "reader-session-secret", "VERY_SECRET_BODY"} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("error leaks %q: %v", secret, err)
				}
			}
		})
	}
}

func TestZAIReaderHonorsContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	reader := newZAIReader("secret-reader-token", server.URL, server.Client())
	if _, err := reader.fetch(ctx, "https://example.com", false); err == nil {
		t.Fatal("expected context error")
	}
}

func sessionHandler(t *testing.T, tool http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	var requestNumber int
	return func(w http.ResponseWriter, r *http.Request) {
		requestNumber++
		switch requestNumber {
		case 1:
			w.Header().Set("Mcp-Session-Id", "reader-session-secret")
			writeSSE(t, w, map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
		case 2:
			w.WriteHeader(http.StatusAccepted)
		case 3:
			tool(w, r)
		default:
			t.Errorf("unexpected request %d", requestNumber)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func toolResult(isError bool, text string) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"result": map[string]any{
			"isError": isError,
			"content": []map[string]any{{"type": "text", "text": text}},
		},
	}
}

func writeSSE(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", body)
}

func assertJSONArgument(t *testing.T, arguments map[string]any, key string, want any) {
	t.Helper()
	if got := arguments[key]; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("argument %s = %#v, want %#v", key, got, want)
	}
}
