package rss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestContentFetcherDeadlineReservesTimeForJina(t *testing.T) {
	for _, tt := range []struct {
		name       string
		reader     string
		slowDirect bool
		wantDirect time.Duration
		wantReader time.Duration
		wantJina   time.Duration
	}{
		{name: "slow direct and reader", reader: "configured", slowDirect: true, wantDirect: 15 * time.Second, wantReader: 15 * time.Second, wantJina: 15 * time.Second},
		{name: "fast direct leaves unused time", reader: "configured", wantDirect: 15 * time.Second, wantReader: 22500 * time.Millisecond, wantJina: 22500 * time.Millisecond},
		{name: "nil reader has two routes", slowDirect: true, wantDirect: 22500 * time.Millisecond, wantJina: 22500 * time.Millisecond},
		{name: "empty key has two routes", reader: "empty", slowDirect: true, wantDirect: 22500 * time.Millisecond, wantJina: 22500 * time.Millisecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				var directBudget, readerBudget, jinaBudget time.Duration
				var readerCalls int
				var noCache bool
				const want = "# Complete Jina article\n\nThe final fallback still has time to retrieve the complete article."
				client := &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					deadline, ok := req.Context().Deadline()
					if !ok {
						return nil, errors.New("attempt has no deadline")
					}
					switch req.URL.Host {
					case "source.invalid":
						directBudget = time.Until(deadline)
						if tt.slowDirect {
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
						return contentBudgetResponse(http.StatusForbidden, ""), nil
					case "reader.invalid":
						readerCalls++
						var call struct {
							Method string `json:"method"`
							Params struct {
								Arguments struct {
									NoCache bool `json:"no_cache"`
								} `json:"arguments"`
							} `json:"params"`
						}
						if err := json.NewDecoder(req.Body).Decode(&call); err != nil {
							return nil, err
						}
						switch call.Method {
						case "initialize":
							response := contentBudgetResponse(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`)
							response.Header.Set("Mcp-Session-Id", "budget-session")
							return response, nil
						case "notifications/initialized":
							return contentBudgetResponse(http.StatusAccepted, ""), nil
						case "tools/call":
							readerBudget = time.Until(deadline)
							noCache = call.Params.Arguments.NoCache
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
					case "jina.invalid":
						jinaBudget = time.Until(deadline)
						if err := req.Context().Err(); err != nil {
							return nil, err
						}
						return contentBudgetResponse(http.StatusOK, want), nil
					}
					return nil, fmt.Errorf("unexpected request %s", req.URL)
				})}
				fetcher := &ContentFetcher{client: client, jinaBaseURL: "https://jina.invalid/"}
				if tt.reader != "" {
					key := ""
					if tt.reader == "configured" {
						key = "reader-key"
					}
					fetcher.reader = newZAIReader(key, "https://reader.invalid/mcp", client)
				}
				got, err := fetcher.FetchContentFreshWithMetadata(ctx, "https://source.invalid/article")
				if err != nil || got.Content != want {
					t.Errorf("final fallback = %q, %v; budgets: direct=%s reader=%s Jina=%s", got.Content, err, directBudget, readerBudget, jinaBudget)
				}
				if directBudget != tt.wantDirect || readerBudget != tt.wantReader || jinaBudget != tt.wantJina {
					t.Errorf("budgets direct=%s reader=%s Jina=%s, want %s %s %s", directBudget, readerBudget, jinaBudget, tt.wantDirect, tt.wantReader, tt.wantJina)
				}
				if tt.reader == "configured" {
					if readerCalls != 3 || !noCache {
						t.Errorf("fresh Reader lifecycle: calls=%d no_cache=%t", readerCalls, noCache)
					}
				} else if readerCalls != 0 {
					t.Errorf("unconfigured Reader received %d calls", readerCalls)
				}
				if err := ctx.Err(); err != nil {
					t.Errorf("fallback exhausted the caller's deadline: %v", err)
				}
			})
		})
	}
}

func TestContentFetcherDeadlineReservesJinaAcrossMultipleReaderRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
		defer cancel()
		callerDeadline, _ := ctx.Deadline()
		var readerMethods []string
		var jinaTimeRemaining time.Duration
		const want = "Complete Jina article after slow MCP requests"
		client := &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "source.invalid":
				return contentBudgetResponse(http.StatusForbidden, ""), nil
			case "reader.invalid":
				var call struct {
					Method string `json:"method"`
				}
				if err := json.NewDecoder(req.Body).Decode(&call); err != nil {
					return nil, err
				}
				readerMethods = append(readerMethods, call.Method)
				if call.Method == "tools/call" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				select {
				case <-time.After(29 * time.Second):
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
				if call.Method == "initialize" {
					response := contentBudgetResponse(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`)
					response.Header.Set("Mcp-Session-Id", "slow-mcp-session")
					return response, nil
				}
				return contentBudgetResponse(http.StatusAccepted, ""), nil
			case "jina.invalid":
				jinaTimeRemaining = time.Until(callerDeadline)
				if err := req.Context().Err(); err != nil {
					return nil, err
				}
				return contentBudgetResponse(http.StatusOK, want), nil
			}
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		})}
		fetcher := &ContentFetcher{client: client, reader: newZAIReader("key", "https://reader.invalid/mcp", client), jinaBaseURL: "https://jina.invalid/"}
		got, err := fetcher.FetchContentFreshWithMetadata(ctx, "https://source.invalid/article")
		if err != nil || got.Content != want {
			t.Errorf("final fallback = %q, %v; Reader methods=%v, remaining=%s", got.Content, err, readerMethods, jinaTimeRemaining)
		}
		if jinaTimeRemaining != 37500*time.Millisecond {
			t.Errorf("Jina time remaining = %s, want 37.5s", jinaTimeRemaining)
		}
		if !reflect.DeepEqual(readerMethods, []string{"initialize", "notifications/initialized"}) {
			t.Errorf("Reader requests exceeded whole-stage deadline: %v", readerMethods)
		}
		if err := ctx.Err(); err != nil {
			t.Errorf("Reader exhausted the caller's deadline: %v", err)
		}
	})
}

func TestContentFetcherPreservesReaderTimeWithAmpleOrNoDeadline(t *testing.T) {
	for _, withDeadline := range []bool{false, true} {
		t.Run(fmt.Sprintf("deadline=%t", withDeadline), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := context.Background()
				if withDeadline {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 6*time.Minute)
					defer cancel()
				}
				readerCalls := 0
				client := &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host == "source.invalid" {
						return contentBudgetResponse(http.StatusForbidden, ""), nil
					}
					if req.URL.Host != "reader.invalid" {
						return nil, fmt.Errorf("unexpected fallback %s", req.URL.Host)
					}
					readerCalls++
					// Three individually fast MCP requests may take more than 30s together.
					select {
					case <-time.After(12 * time.Second):
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
					switch readerCalls {
					case 1:
						response := contentBudgetResponse(http.StatusOK, `{"jsonrpc":"2.0","id":1,"result":{}}`)
						response.Header.Set("Mcp-Session-Id", "worker-session")
						return response, nil
					case 2:
						return contentBudgetResponse(http.StatusAccepted, ""), nil
					default:
						return contentBudgetResponse(http.StatusOK, `{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"{\"content\":\"Complete worker article\"}"}]}}`), nil
					}
				})}
				fetcher := &ContentFetcher{client: client, reader: newZAIReader("key", "https://reader.invalid/mcp", client), jinaBaseURL: "https://jina.invalid/"}
				got, err := fetcher.FetchContent(ctx, "https://source.invalid/article")
				if err != nil || got != "Complete worker article" || readerCalls != 3 {
					t.Fatalf("worker extraction = %q, %v; Reader calls=%d", got, err, readerCalls)
				}
			})
		})
	}
}

func TestContentFetcherCancellationStopsFurtherAttempts(t *testing.T) {
	for _, cancelAt := range []string{"before first attempt", "source.invalid", "reader.invalid", "jina.invalid"} {
		t.Run(cancelAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelAt == "before first attempt" {
				cancel()
			}
			var calls []string
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls = append(calls, req.URL.Host)
				if req.URL.Host == cancelAt {
					cancel()
				}
				return contentBudgetResponse(http.StatusServiceUnavailable, ""), nil
			})}
			fetcher := &ContentFetcher{client: client, reader: newZAIReader("key", "https://reader.invalid/mcp", client), jinaAPIKey: "key", jinaBaseURL: "https://jina.invalid/"}
			got, err := fetcher.FetchContent(ctx, "https://source.invalid/article")
			if !errors.Is(err, context.Canceled) || got != "" {
				t.Errorf("cancelled extraction = %q, %v", got, err)
			}
			var want []string
			if cancelAt != "before first attempt" {
				for _, host := range []string{"source.invalid", "reader.invalid", "jina.invalid"} {
					want = append(want, host)
					if host == cancelAt {
						break
					}
				}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Errorf("attempts after cancellation: got %v, want %v", calls, want)
			}
		})
	}
}

func TestContentFetcherExpiredDeadlineIsNotEmptySuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		client := &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "source.invalid" {
				return contentBudgetResponse(http.StatusForbidden, ""), nil
			}
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		fetcher := &ContentFetcher{client: client, reader: newZAIReader("key", "https://reader.invalid/mcp", client), jinaBaseURL: "https://jina.invalid/"}
		got, err := fetcher.FetchContent(ctx, "https://source.invalid/article")
		if !errors.Is(err, context.DeadlineExceeded) || got != "" {
			t.Fatalf("expired extraction = %q, %v", got, err)
		}
	})
}

func TestContentFetcherFastDirectSuccessWithDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	want := strings.Repeat("Complete direct article paragraph. ", 20)
	calls := 0
	client := &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "source.invalid" {
			return nil, fmt.Errorf("unexpected fallback %s", req.URL.Host)
		}
		return contentBudgetResponse(http.StatusOK, "<html><head><title>Direct title</title></head><body><article><p>"+want+"</p></article></body></html>"), nil
	})}
	fetcher := &ContentFetcher{client: client, reader: newZAIReader("key", "https://reader.invalid/mcp", client), jinaBaseURL: "https://jina.invalid/"}
	got, err := fetcher.FetchContentFreshWithMetadata(ctx, "https://source.invalid/article")
	if err != nil || got.Content != strings.TrimSpace(want) || got.Title != "Direct title" || calls != 1 {
		t.Fatalf("direct extraction = %#v, %v; calls=%d", got, err, calls)
	}
}

func contentBudgetResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
