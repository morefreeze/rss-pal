package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/rss-pal/internal/taskbudget"
	"github.com/gin-gonic/gin"
)

func writeTaskBudgetError(c *gin.Context, err error) bool {
	if !taskbudget.IsDenied(err) {
		return false
	}
	code := http.StatusServiceUnavailable
	if errors.Is(err, taskbudget.ErrExceeded) {
		code = http.StatusTooManyRequests
		c.Header("Retry-After", "60")
	}
	c.AbortWithStatusJSON(code, gin.H{"error": err.Error()})
	return true
}

// TaskBudgetMiddleware follows identity resolution, before any external work.
// All expensive routes share a per-user concurrent request limit. AI's actual
// upstream calls are additionally metered in Summarizer (stream/vision too).
func TaskBudgetMiddleware(store *taskbudget.Store, policies taskbudget.Policies) gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := getUserID(c)
		if uid <= 0 {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		ctx := taskbudget.WithOwner(c.Request.Context(), uid)
		c.Request = c.Request.WithContext(ctx)
		bucket, cost := taskRoute(c.Request.Method, c.FullPath()), 1
		// Taking an entry down performs no fetch and must remain possible even
		// when the administrator's fetch budget is exhausted.
		if c.FullPath() == "/api/admin/feed-catalog/:id/publication" {
			body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
			if err != nil {
				c.AbortWithStatusJSON(413, gin.H{"error": "请求内容过大"})
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			var in struct {
				Published *bool `json:"published"`
			}
			if json.Unmarshal(body, &in) == nil && in.Published != nil && !*in.Published {
				bucket = ""
			}
		}

		if bucket == "" {
			c.Next()
			return
		}
		maxBody := int64(4 << 20)
		if c.FullPath() == "/api/bookmarklet/capture-pdf" {
			maxBody = 65 << 20
		}
		if c.FullPath() == "/api/settings/polish-prompt" {
			maxBody = 64 << 10
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBody)
		}
		if c.FullPath() == "/api/extension/ingest" || c.FullPath() == "/api/explore/sources/subscribe-batch" {
			body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20))
			if err != nil {
				c.AbortWithStatusJSON(413, gin.H{"error": "请求内容过大"})
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			var batch struct {
				Items     []json.RawMessage `json:"items"`
				SourceIDs []int             `json:"source_ids"`
			}
			if json.Unmarshal(body, &batch) != nil {
				c.AbortWithStatusJSON(400, gin.H{"error": "无效请求"})
				return
			}
			if len(batch.Items) > cost {
				cost = len(batch.Items)
			}
			if len(batch.SourceIDs) > cost {
				cost = len(batch.SourceIDs)
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		c.Request = c.Request.WithContext(requestCtx)
		release, err := store.Acquire(requestCtx, uid, "interactive", 1, policies["interactive"])
		if err != nil {
			writeTaskBudgetError(c, err)
			return
		}
		defer release()
		if bucket != "interactive" {
			done, err := store.Acquire(requestCtx, uid, bucket, cost, policies[bucket])
			if err != nil {
				writeTaskBudgetError(c, err)
				return
			}
			defer done()
		}
		c.Next()
	}
}
func taskRoute(method, path string) string {
	if method == "GET" && path == "/api/articles/:id/candidates" {
		return "fetch"
	}
	if method != "POST" {
		return ""
	}
	switch path {
	case "/api/bookmarklet/capture-pdf", "/api/bookmarklet/capture-pdf-url":
		return "pdf"
	case "/api/bookmarklet/capture", "/api/extension/ingest", "/api/feeds/oneoff_link_set":
		return "capture"
	case "/api/feeds", "/api/explore/sources/subscribe-batch", "/api/explore/sources/:id/subscribe":
		return "subscribe"
	case "/api/admin/feed-catalog/:id/check", "/api/admin/feed-catalog/:id/publication", "/api/feeds/preview", "/api/feeds/:id/fetch", "/api/articles/:id/content", "/api/articles/:id/expand", "/api/articles/:id/batch_fetch", "/api/articles/:id/confirm_link_set", "/api/articles/:id/youtube-playback":
		return "fetch"
	case "/api/articles/:id/summary", "/api/settings/polish-prompt":
		return "interactive"
	}
	if strings.Contains(path, "interests/generate") || strings.Contains(path, "insights/generate") {
		return "interactive"
	}
	return ""
}
