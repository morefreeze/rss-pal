package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestExpandChildRequeuesFailedFetch(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanApp := testdb.NewAsApp(t, schema)
	defer cleanApp()

	var owner, other, feed, parent int
	for username, id := range map[string]*int{"link-owner": &owner, "other-user": &other} {
		if err := db.QueryRow("INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id", username).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow("INSERT INTO feeds(url,title,owner_id) VALUES('https://example.org/feed','Links',$1) RETURNING id", owner).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("INSERT INTO articles(feed_id,title,url) VALUES($1,'Parent','https://example.org/parent') RETURNING id", feed).Scan(&parent); err != nil {
		t.Fatal(err)
	}
	addArticle := func(path, state string, parentID *int, attempts int, failure string) int {
		t.Helper()
		var id int
		err := db.QueryRow(`
			INSERT INTO articles(feed_id, title, url, parent_article_id, processing_state, refetch_attempts, processing_error)
			VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id
		`, feed, path, "https://example.org/"+path, parentID, state, attempts, failure).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}

	failed := addArticle("failed", "failed", &parent, 3, "fetch timed out")
	stub := addArticle("stub", "stub", &parent, 0, "")
	ready := addArticle("ready", "ready", &parent, 0, "")
	failedPDF := addArticle("failed.pdf", "failed", nil, 3, "OCR failed")

	router := gin.New()
	handler := NewArticleHandler(repository.NewArticleRepository(app), nil, nil, nil, nil, nil, nil)
	router.POST("/api/articles/:id/expand", func(c *gin.Context) {
		userID := owner
		if c.GetHeader("X-Test-User") == "other" {
			userID = other
		}
		c.Set("userID", userID)
	}, RLSTxMiddleware(app), handler.ExpandChild)

	expand := func(id int, user string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/articles/%d/expand", id), nil)
		req.Header.Set("X-Test-User", user)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("expand %d: status=%d want=%d body=%s", id, res.Code, want, res.Body.String())
		}
		if want == http.StatusOK {
			var response struct {
				ArticleID int    `json:"article_id"`
				State     string `json:"state"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &response); err != nil || response.ArticleID != id || response.State != "processing" {
				t.Fatalf("expand %d: response=%+v err=%v", id, response, err)
			}
		}
	}

	expand(failed, "other", http.StatusNotFound)
	expand(failed, "owner", http.StatusOK)
	expand(failed, "owner", http.StatusConflict)
	expand(stub, "owner", http.StatusOK)
	expand(ready, "owner", http.StatusConflict)
	expand(failedPDF, "owner", http.StatusConflict)

	children, err := repository.NewArticleRepository(db).GetProcessingChildren(10)
	if err != nil || len(children) != 2 || children[0].ID != failed || children[1].ID != stub {
		t.Fatalf("worker queue: children=%+v err=%v", children, err)
	}
	var attempts int
	var failure string
	err = db.QueryRow("SELECT refetch_attempts, processing_error FROM articles WHERE id=$1", failed).Scan(&attempts, &failure)
	if err != nil || attempts != 0 || failure != "" {
		t.Fatalf("retry state: attempts=%d failure=%q err=%v", attempts, failure, err)
	}
}
