package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

type hnCaptureFetcher struct {
	target string
	err    error
}

func (f *hnCaptureFetcher) FetchContentWithMetadata(_ context.Context, target string) (rss.ContentResult, error) {
	f.target = target
	return rss.ContentResult{Title: "HN title", Content: "HN body and ranked discussion"}, f.err
}
func TestCaptureHNUsesAPIForBookmarkAndExtension(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			h, repo := newTestBookmarkletHandlerForPDF(t)
			fetcher := &hnCaptureFetcher{}
			if fail {
				fetcher.err = fmt.Errorf("HN unavailable")
			}
			h.hnFetcher = fetcher
			router := gin.New()
			router.POST("/capture", h.Capture)
			body, _ := json.Marshal(map[string]any{"url": "https://news.ycombinator.com/item?id=123&utm_source=test#c", "title": "page chrome", "html": "<body>navigation and incomplete page</body>"})
			req := httptest.NewRequest("POST", "/capture", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if fail {
				if w.Code != http.StatusBadGateway || len(repo.created) != 0 {
					t.Fatalf("failure persisted: %d %v", w.Code, repo.created)
				}
				return
			}
			if w.Code != 201 || len(repo.created) != 1 {
				t.Fatalf("capture=%d %s", w.Code, w.Body.String())
			}
			a := repo.created[0]
			if a.Title != "HN title" || a.Content != "HN body and ranked discussion" || a.URL != "https://news.ycombinator.com/item?id=123" {
				t.Fatalf("bad article=%+v", a)
			}
		})
	}
}
