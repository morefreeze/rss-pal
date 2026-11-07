package rss

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHNDirectBeforeProxy(t *testing.T) {
	f := hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Proxy content"}`})
	var proxyCalls atomic.Int32
	proxy := f.client.Transport
	f.client.Transport = hnTransport(func(r *http.Request) (*http.Response, error) { proxyCalls.Add(1); return proxy.RoundTrip(r) })
	f.hnDirectClient = hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Direct content","kids":[124]}`, "/v0/item/124.json": `{"id":124,"type":"comment","by":"reader","text":"Direct discussion"}`}).client
	got, err := f.FetchContentWithMetadata(context.Background(), "https://news.ycombinator.com/item?id=123&utm_term=comment")
	if err != nil || got.Title != "Direct content" || !strings.Contains(got.Content, "Direct discussion") || proxyCalls.Load() != 0 {
		t.Fatalf("title=%s err=%v proxyCalls=%d", got.Title, err, proxyCalls.Load())
	}
}

func TestHNDirectFailureFallsBackToProxy(t *testing.T) {
	for _, failure := range []string{"transport", "status", "body", "json", "timeout"} {
		t.Run(failure, func(t *testing.T) {
			f := hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Fallback","kids":[124]}`, "/v0/item/124.json": `{"id":124,"type":"comment","by":"reader","text":"Fallback discussion"}`})
			var directCalls atomic.Int32
			f.hnDirectClient = &http.Client{Transport: hnTransport(func(r *http.Request) (*http.Response, error) {
				directCalls.Add(1)
				switch failure {
				case "transport":
					return nil, fmt.Errorf("direct reset")
				case "timeout":
					return nil, context.DeadlineExceeded
				case "status":
					return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("unavailable"))}, nil
				case "body":
					return &http.Response{StatusCode: 200, Body: io.NopCloser(hnBrokenReader{})}, nil
				default:
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("not JSON"))}, nil
				}
			})}
			got, err := f.FetchContentWithMetadata(context.Background(), "https://news.ycombinator.com/item?id=123")
			if err != nil || got.Title != "Fallback" || !strings.Contains(got.Content, "Fallback discussion") || directCalls.Load() != 2 {
				t.Fatalf("title=%s err=%v directCalls=%d", got.Title, err, directCalls.Load())
			}
		})
	}
}

type hnBrokenReader struct{}

func (hnBrokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestHNDirectUnavailableDoesNotUseProxy(t *testing.T) {
	f := hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Stale proxy"}`})
	f.hnDirectClient = hnTestFetcher(map[string]string{"/v0/item/123.json": `null`}).client
	_, err := f.FetchContent(context.Background(), "https://news.ycombinator.com/item?id=123")
	if !errors.Is(err, ErrHackerNewsUnavailable) {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestHNDirectParentCancellationDoesNotUseProxy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Unexpected fallback"}`})
	f.hnDirectClient = &http.Client{Transport: hnTransport(func(r *http.Request) (*http.Response, error) { cancel(); return nil, ctx.Err() })}
	_, err := f.FetchContent(ctx, "https://news.ycombinator.com/item?id=123")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
}

func TestHNDirectTimeoutLeavesTimeForProxy(t *testing.T) {
	f := hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Fallback"}`})
	f.hnDirectClient = &http.Client{Transport: hnTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := f.FetchContentWithMetadata(ctx, "https://news.ycombinator.com/item?id=123")
	if err != nil || got.Title != "Fallback" {
		t.Fatalf("title=%s err=%v", got.Title, err)
	}
}

func TestHNDirectConnectionRecoveryBeforeProxy(t *testing.T) {
	f := hnTestFetcher(map[string]string{}) // Any proxy call fails this test.
	f.hnDirectClient = &http.Client{Transport: hnTransport(func(r *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})}
	f.hnRecoveryClient = hnTestFetcher(map[string]string{"/v0/item/123.json": `{"id":123,"type":"story","title":"Recovered direct"}`}).client
	got, err := f.FetchContentWithMetadata(context.Background(), "https://news.ycombinator.com/item?id=123")
	if err != nil || got.Title != "Recovered direct" {
		t.Fatalf("title=%s err=%v", got.Title, err)
	}
}
