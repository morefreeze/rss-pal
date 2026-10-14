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
	for _, failure := range []error{nil, fmt.Errorf("HN unavailable"), rss.ErrHackerNewsUnavailable} {
		fail := failure != nil
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			h, repo := newTestBookmarkletHandlerForPDF(t)
			fetcher := &hnCaptureFetcher{}
			fetcher.err = failure
			h.hnFetcher = fetcher
			router := gin.New()
			router.POST("/capture", h.Capture)
			body, _ := json.Marshal(map[string]any{"url": "https://news.ycombinator.com/item?id=123&utm_source=test#c", "title": "page chrome", "html": "<body>navigation and incomplete page</body>"})
			req := httptest.NewRequest("POST", "/capture", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer test-token")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if fail {
				status := http.StatusBadGateway
				if failure == rss.ErrHackerNewsUnavailable {
					status = http.StatusUnprocessableEntity
				}
				if w.Code != status || len(repo.created) != 0 {
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
