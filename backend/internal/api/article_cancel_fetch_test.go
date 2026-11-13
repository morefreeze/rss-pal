package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/gin-gonic/gin"
)

func TestCancelLinkFetch(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, cleanApp := testdb.NewAsApp(t, schema)
	defer cleanApp()

	var owner, other, feed int
	for name, id := range map[string]*int{"owner": &owner, "other": &other} {
		if err := db.QueryRow("INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id", name).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow("INSERT INTO feeds(url,title,owner_id) VALUES('https://example.org/feed','Feed',$1) RETURNING id", owner).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	add := func(path, state string, parent *int) int {
		t.Helper()
		var id int
		err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,parent_article_id,processing_state)
			VALUES($1,$2,$3,$4,$5) RETURNING id`, feed, path, "https://example.org/"+path, parent, state).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	parent := add("parent", "ready", nil)
	wrongParent := add("wrong-parent", "ready", nil)
	processing := add("processing", "processing", &parent)
	failed := add("failed", "failed", &parent)
	ready := add("ready", "ready", &parent)
	standalone := add("standalone", "failed", nil)

	router := gin.New()
	h := NewArticleHandler(repository.NewArticleRepository(app), nil, nil, nil, nil, nil, nil)
	router.DELETE("/api/articles/:id/fetch-tasks/:childId", func(c *gin.Context) {
		user := owner
		if c.GetHeader("X-Test-User") == "other" {
			user = other
		}
		c.Set("userID", user)
	}, RLSTxMiddleware(app), h.CancelLinkFetch)
	cancel := func(parent, child int, user string, want int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/articles/%d/fetch-tasks/%d", parent, child), nil)
		req.Header.Set("X-Test-User", user)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("cancel %d: status=%d want=%d body=%s", child, res.Code, want, res.Body.String())
		}
	}
	cancel(parent, processing, "other", http.StatusNotFound)
	cancel(wrongParent, processing, "owner", http.StatusNotFound)
	cancel(parent, standalone, "owner", http.StatusNotFound)
	cancel(parent, ready, "owner", http.StatusConflict)
	cancel(parent, processing, "owner", http.StatusNoContent)
	cancel(parent, failed, "owner", http.StatusNoContent)
	cancel(parent, processing, "owner", http.StatusNotFound)
	repo := repository.NewArticleRepository(db)
	if saved, err := repo.CompleteLinkFetch(processing, "late result", "late content", 1, 1); err != nil || saved {
		t.Fatalf("canceled task accepted worker write: saved=%v err=%v", saved, err)
	}

	urls, err := repo.GetChildURLs(parent)
	if err != nil || len(urls) != 1 || urls[0] != "https://example.org/ready" {
		t.Fatalf("remaining child URLs = %v, err = %v", urls, err)
	}
	if inserted, err := repo.EnableAndInsertLinkSetChildren(parent, []repository.LinkSetChildInput{{
		FeedID: feed, ParentArticleID: parent, URL: "https://example.org/processing", Title: "Resubmitted", ProcessingState: "processing",
	}}); err != nil || inserted != 1 {
		t.Fatalf("resubmit canceled URL: inserted=%d err=%v", inserted, err)
	}
	if err := repo.SetLinksExtendable(parent, false); err != nil {
		t.Fatal(err)
	}
	parentArticle, err := repo.GetByID(parent, owner)
	if err != nil || parentArticle.LinksExtendable == nil || !*parentArticle.LinksExtendable {
		t.Fatalf("late detection hid submitted tasks: article=%+v err=%v", parentArticle, err)
	}

}
