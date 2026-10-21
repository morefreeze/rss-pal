package main

import (
	"context"
	"fmt"
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestFailedSubscriptionWaitsBeforeRetry(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	old := workerBudgets
	workerBudgets = nil
	defer func() { workerBudgets = old }()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	if _, err := db.Exec(`INSERT INTO feeds(url,title,feed_type,status) VALUES($1,'failure','rss','active')`, server.URL); err != nil {
		t.Fatal(err)
	}
	repo := repository.NewFeedRepository(db)
	for i := 0; i < 2; i++ {
		fetchAllFeeds(context.Background(), repo, nil, rss.NewFetcher(""), nil, nil)
	}
	if calls.Load() != 1 {
		t.Fatalf("failed source requested %d times without backoff", calls.Load())
	}
}
func TestSubscriptionDrainsBatchBeforeCachingValidators(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	old := workerBudgets
	workerBudgets = nil
	defer func() { workerBudgets = old }()
	total := maxNewArticlesPerFeed + 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"batch"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"batch"`)
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Baseline</title><link>https://example.org</link><description>Feed</description>`)
		for i := 0; i < total; i++ {
			fmt.Fprintf(w, `<item><title>Article %d</title><link>https://example.org/%d</link><description>Body %d</description></item>`, i, i, i)
		}
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer server.Close()
	repo := repository.NewFeedRepository(db)
	articles := repository.NewArticleRepository(db)
	feed := model.Feed{URL: server.URL, FeedType: "podcast", FetchIntervalMin: 60}
	if err := repo.Create(&feed); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		current, err := repo.GetByID(feed.ID)
		if err != nil {
			t.Fatal(err)
		}
		processFeed(context.Background(), repo, articles, rss.NewFetcher(""), nil, nil, *current)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE feed_id=$1`, feed.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != total {
		t.Fatalf("ingested %d/%d: cached validators lost the tail", count, total)
	}
	var etag string
	db.QueryRow(`SELECT etag FROM feeds WHERE id=$1`, feed.ID).Scan(&etag)
	if etag != `"batch"` {
		t.Fatalf("validators not committed after drain: %q", etag)
	}
}

func TestSubscriptionBaselineRSSDatabaseAndArticleAPI(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	old := workerBudgets
	workerBudgets = nil
	defer func() { workerBudgets = old }()
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('baseline-reader','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	body := `<h2>完整正文</h2><p>` + strings.Repeat("长文段落。", 120) + `</p><table><tr><th>项目</th><th>数值</th></tr><tr><td>可靠性</td><td>42</td></tr></table><pre><code>const answer = 42;</code></pre><img src="https://example.org/image.png"><p>正文结束标记</p>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"baseline"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"baseline"`)
		fmt.Fprint(w, `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>Baseline</title><link>https://example.org</link><description>Baseline</description>`)
		for i := 0; i < 13; i++ {
			fmt.Fprintf(w, `<item><title>Baseline %d</title><link>https://example.org/%d</link><content:encoded><![CDATA[%s]]></content:encoded></item>`, i, i, body)
		}
		fmt.Fprint(w, `</channel></rss>`)
	}))
	defer server.Close()
	feeds := repository.NewFeedRepository(db)
	articles := repository.NewArticleRepository(db)
	feed := model.Feed{URL: server.URL, OwnerID: &owner, FeedType: "podcast", FetchIntervalMin: 60}
	if err := feeds.Create(&feed); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := db.Exec(`UPDATE feeds SET next_attempt_at=NOW()-interval '1 second' WHERE id=$1`, feed.ID); err != nil {
			t.Fatal(err)
		}
		fetchAllFeeds(context.Background(), feeds, articles, rss.NewFetcher(""), nil, nil)
	}
	var aid, count int
	db.QueryRow(`SELECT min(id),count(*) FROM articles WHERE feed_id=$1`, feed.ID).Scan(&aid, &count)
	if count != 13 {
		t.Fatalf("baseline imported=%d want13", count)
	}
	h := api.NewArticleHandler(repository.NewArticleRepository(app), repository.NewArticleUserTagRepository(app), repository.NewProgressRepository(app), repository.NewPreferenceRepository(app), repository.NewHiddenArticleRepository(app), nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", owner) }, api.RLSTxMiddleware(app))
	router.GET("/articles", h.GetAll)
	router.GET("/articles/:id", h.GetByID)
	for _, path := range []string{"/articles", fmt.Sprintf("/articles/%d", aid)} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if path != "/articles" {
			for _, want := range []string{"正文结束标记", "answer = 42", "image.png", "可靠性"} {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("API lost %q", want)
				}
			}
		}
	}
	// A repeated conditional check must retain all rows and their full content.
	current, err := feeds.GetByID(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	processFeed(context.Background(), feeds, articles, rss.NewFetcher(""), nil, nil, *current)
	var after int
	db.QueryRow(`SELECT count(*) FROM articles WHERE feed_id=$1`, feed.ID).Scan(&after)
	if after != 13 {
		t.Fatalf("duplicate/lost rows: %d", after)
	}
}

func TestSubscriptionDoesNotAcknowledgeFailedIngestion(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	old := workerBudgets
	workerBudgets = nil
	defer func() { workerBudgets = old }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"storage"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"storage"`)
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Storage</title><link>https://example.org</link><description>Storage</description><item><title>Fail once</title><link>https://example.org/retry</link><description>Body</description></item></channel></rss>`)
	}))
	defer server.Close()
	feeds := repository.NewFeedRepository(db)
	articles := repository.NewArticleRepository(db)
	feed := model.Feed{URL: server.URL, FeedType: "podcast", FetchIntervalMin: 60}
	if err := feeds.Create(&feed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`ALTER TABLE articles ADD CONSTRAINT simulate_storage_failure CHECK(title <> 'Fail once')`); err != nil {
		t.Fatal(err)
	}
	processFeed(context.Background(), feeds, articles, rss.NewFetcher(""), nil, nil, feed)
	failed, err := feeds.GetByID(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.ETag != "" || failed.LastFetchedAt != nil || failed.FetchError == "" {
		t.Fatalf("failed ingestion was acknowledged: %+v", failed)
	}
	if _, err := db.Exec(`ALTER TABLE articles DROP CONSTRAINT simulate_storage_failure`); err != nil {
		t.Fatal(err)
	}
	processFeed(context.Background(), feeds, articles, rss.NewFetcher(""), nil, nil, *failed)
	recovered, err := feeds.GetByID(feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ETag != `"storage"` || recovered.FetchError != "" {
		t.Fatalf("recovery failed: %+v", recovered)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM articles WHERE feed_id=$1`, feed.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("stored=%d want1", n)
	}
}
