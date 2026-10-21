package api

import (
	"database/sql"
	"fmt"
	"github.com/bytedance/rss-pal/internal/ai"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func summaryTestRouter(t *testing.T, provider string, sabotage bool) (*gin.Engine, *sql.DB, int) {
	t.Helper()
	db, cleanup := testdb.New(t)
	t.Cleanup(cleanup)
	var user, feed, article int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('summary-owner','x') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://summary.example/feed','Summary',$1) RETURNING id`, user).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed) VALUES($1,'Title','https://summary.example/article','article body','prior brief','prior detailed') RETURNING id`, feed).Scan(&article); err != nil {
		t.Fatal(err)
	}
	if sabotage {
		if _, err := db.Exec(`CREATE TABLE summary_commit_probe(uid integer REFERENCES users(id) DEFERRABLE INITIALLY DEFERRED)`); err != nil {
			t.Fatal(err)
		}
	}
	repo := repository.NewArticleRepository(db)
	handler := NewArticleHandler(repo, nil, nil, nil, nil, service.NewSummarizerService(ai.NewSummarizerWithModel("k", provider, "test")), nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", user) }, RLSTxMiddleware(db))
	router.POST("/articles/:id/summary", func(c *gin.Context) {
		if sabotage {
			if _, err := c.MustGet(CtxKeyTx).(*sql.Tx).Exec(`INSERT INTO summary_commit_probe VALUES(-999)`); err != nil {
				t.Fatal(err)
			}
		}
		handler.GenerateSummary(c)
	})
	return router, db, article
}

func TestSummaryConcurrentRequestsSharePersistedLease(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"valid summary"},"finish_reason":"stop"}]}`)
	}))
	defer provider.Close()
	router, _, id := summaryTestRouter(t, provider.URL, false)
	path := fmt.Sprintf("/articles/%d/summary", id)
	first := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { router.ServeHTTP(first, httptest.NewRequest("POST", path, nil)); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider not called")
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest("POST", path, nil))
	if second.Code != 409 || !strings.Contains(second.Body.String(), "summary_in_progress") {
		t.Errorf("duplicate: %d %s", second.Code, second.Body)
	}
	close(release)
	<-done
	if first.Code != 200 || calls.Load() != 2 {
		t.Fatalf("first %d %s calls=%d", first.Code, first.Body, calls.Load())
	}
}

func TestSummaryCommitFailureNeverEmitsSuccess(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Accept"), "event-stream") {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"valid summary\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"valid summary"},"finish_reason":"stop"}]}`)
		}
	}))
	defer provider.Close()
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			router, db, id := summaryTestRouter(t, provider.URL, true)
			path := fmt.Sprintf("/articles/%d/summary", id)
			if stream {
				path += "?stream=1"
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest("POST", path, nil))
			if stream {
				if strings.Contains(w.Body.String(), `"type":"done"`) || !strings.Contains(w.Body.String(), `"type":"error"`) {
					t.Fatalf("stream falsely completed: %s", w.Body)
				}
			} else if w.Code != 500 {
				t.Fatalf("JSON falsely completed: %d %s", w.Code, w.Body)
			}
			var summary, state string
			if err := db.QueryRow(`SELECT a.summary_brief,j.state FROM articles a JOIN article_summary_jobs j ON j.article_id=a.id WHERE a.id=$1`, id).Scan(&summary, &state); err != nil {
				t.Fatal(err)
			}
			if summary != "prior brief" || state != "failed" {
				t.Fatalf("summary=%q job=%q", summary, state)
			}
		})
	}
}

func TestSummaryEmptyOutputPreservesPriorSummary(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"  "},"finish_reason":"stop"}]}`)
	}))
	defer provider.Close()
	router, db, id := summaryTestRouter(t, provider.URL, false)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", fmt.Sprintf("/articles/%d/summary", id), nil))
	if w.Code != 500 {
		t.Fatalf("accepted empty summary %d %s", w.Code, w.Body)
	}
	var summary, state string
	if err := db.QueryRow(`SELECT a.summary_brief,j.state FROM articles a JOIN article_summary_jobs j ON j.article_id=a.id WHERE a.id=$1`, id).Scan(&summary, &state); err != nil {
		t.Fatal(err)
	}
	if summary != "prior brief" || state != "failed" {
		t.Fatalf("summary=%q state=%q", summary, state)
	}
}
