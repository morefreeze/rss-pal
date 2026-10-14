package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/gin-gonic/gin"
)

func TestCapturePublicationDate(t *testing.T) {
	for _, tc := range []struct{ name, metadata, want string }{
		{"lilian meta", `<meta property="article:published_time" content="2023-06-23T00:00:00+00:00">`, "2023-06-23T00:00:00Z"},
		{"json ld", `<script type="application/ld+json">{"@type":"BlogPosting","datePublished":"2023-06-23T00:00:00Z"}</script>`, "2023-06-23T00:00:00Z"},
		{"modified is not published", `<meta property="article:modified_time" content="2023-06-23T00:00:00Z">`, ""},
		{"no date", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo := newTestBookmarkletHandlerForPDF(t)
			r := gin.New()
			r.POST("/capture", h.Capture)
			payload, _ := json.Marshal(map[string]any{"url": "https://lilianweng.github.io/posts/2023-06-23-agent/", "title": "Agents", "html": "<html><head>" + tc.metadata + "</head><body><article><p>" + strings.Repeat("Article body. ", 30) + "</p></article></body></html>"})
			req := httptest.NewRequest("POST", "/capture", bytes.NewReader(payload))
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != 201 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			got := repo.created[0].PublishedAt
			if tc.want == "" {
				if got != nil {
					t.Fatalf("unexpected date %v", got)
				}
				return
			}
			if got == nil || got.UTC().Format(time.RFC3339) != tc.want {
				t.Fatalf("published_at = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestRecaptureFillsOnlyMissingPublicationDate(t *testing.T) {
	for _, hasDate := range []bool{false, true} {
		h, repo := newTestBookmarkletHandlerForPDF(t)
		url := "https://example.com/a"
		existing := &model.Article{ID: 44, FeedID: 7, URL: url, Title: "article", Content: "old"}
		old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
		if hasDate {
			existing.PublishedAt = &old
		}
		repo.byOwnerAndURL["42|"+url] = existing
		repo.byID[44] = existing
		r := gin.New()
		r.POST("/capture", h.Capture)
		payload, _ := json.Marshal(map[string]any{"url": url, "title": "article", "force": true, "html": `<html><head><meta property="article:published_time" content="2023-06-23"></head><body><article><p>` + strings.Repeat("Article body. ", 30) + `</p></article></body></html>`})
		req := httptest.NewRequest("POST", "/capture", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer test-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		want := "2023-06-23"
		if hasDate {
			want = "2001-01-01"
		}
		if existing.PublishedAt == nil || existing.PublishedAt.Format("2006-01-02") != want {
			t.Fatalf("published_at %v want %s", existing.PublishedAt, want)
		}
	}
}
