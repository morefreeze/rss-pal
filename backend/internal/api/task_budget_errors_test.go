package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"github.com/gin-gonic/gin"
)

func TestBudgetDenialsReachJSONAndSummaryStream(t *testing.T) {
	for _, reason := range []string{"user_daily", "global_daily", "user_concurrency", "global_concurrency"} {
		t.Run(reason, func(t *testing.T) {
			db, done := testdb.New(t)
			defer done()
			p := taskbudget.Policy{Daily: 10, GlobalDaily: 10, Concurrent: 10, GlobalConcurrent: 10, Lease: time.Minute}
			switch reason {
			case "user_daily":
				p.Daily = 1
			case "global_daily":
				p.GlobalDaily = 1
			case "user_concurrency":
				p.Concurrent = 1
			case "global_concurrency":
				p.GlobalConcurrent = 1
			}
			store := taskbudget.New(db)
			release, err := store.Acquire(context.Background(), 1, "ai", 1, p)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			_, denied := store.Acquire(context.Background(), 1, "ai", 1, p)
			if denied == nil {
				t.Fatal("expected denial")
			}
			for _, stream := range []bool{false, true} {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("POST", "/api/articles/2925/summary?stream=1", nil)
				if stream {
					summarizer := ai.NewSummarizerWithModel("unused", "http://127.0.0.1:1", "test")
					summarizer.SetAdmission(func(context.Context) (func(), error) { return nil, denied })
					(&ArticleHandler{}).streamSummary(c, 2925, &model.Article{Title: "video", Content: "## 字幕\nExisting transcript"}, service.NewSummarizerService(summarizer), 0)
				} else if !writeTaskBudgetError(c, fmt.Errorf("failed to stream brief: %w", denied)) {
					t.Fatal("not handled")
				}
				var data map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
					t.Fatalf("decode %s: %v", w.Body.String(), err)
				}
				if data["reason"] != reason || data["bucket"] != "ai" || data["used"] != float64(1) || data["limit"] != float64(1) {
					t.Errorf("stream=%v missing denial details: %s", stream, w.Body.String())
				}
				key := "error"
				if stream {
					key = "msg"
					if data["type"] != "error" {
						t.Error("missing error frame")
					}
				} else if w.Code != 429 {
					t.Errorf("status=%d", w.Code)
				}
				msg, _ := data[key].(string)
				if strings.Contains(msg, "failed to") || strings.Contains(msg, "或已有") {
					t.Errorf("internal/ambiguous message: %s", msg)
				}
				scope := "个人"
				if strings.HasPrefix(reason, "global") {
					scope = "全站"
				}
				if !strings.Contains(msg, scope) {
					t.Errorf("missing scope: %s", msg)
				}
				if strings.HasSuffix(reason, "daily") {
					if !strings.Contains(msg, "每日") || !strings.Contains(msg, "北京时间") || data["retry_at"] == nil {
						t.Errorf("missing reset: %s", w.Body.String())
					}
					if !stream && w.Header().Get("Retry-After") == "60" {
						t.Error("daily reset incorrectly says 60 seconds")
					}
				} else {
					if !strings.Contains(msg, "并发") || data["retry_at"] != nil {
						t.Errorf("wrong concurrency message: %s", w.Body.String())
					}
				}
			}
		})
	}
}
