package explorecontent

import (
	"context"
	"errors"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/taskbudget"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type contentTransport func(*http.Request) (*http.Response, error)

func (f contentTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestArticleContentCache(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var sid, id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,category,language) VALUES ('https://8.8.8.8/feed','test','https://8.8.8.8/feed','test','en') RETURNING id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES ($1,'https://8.8.8.8/page','https://8.8.8.8/page','test','intro',NOW()) RETURNING id`, sid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client := &http.Client{Transport: contentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(`<article><p>` + strings.Repeat("Long original body. ", 30) + `</p><a href="/tail">tail</a></article>`)), Request: r}, nil
	})}
	service := NewArticleContentService(db, client)
	intro := "intro"
	detail := &repository.ExploreArticleDetail{ID: id, URL: "https://8.8.8.8/page", Content: &intro}
	charges := 0
	charge := func() (func(), error) { charges++; return func() {}, nil }
	if err := service.Fetch(context.Background(), detail, charge); err != nil {
		t.Fatal(err)
	}
	if detail.ContentStatus != "fetched" || !strings.Contains(*detail.Content, "https://8.8.8.8/tail") {
		t.Fatalf("detail=%+v body=%s", detail, *detail.Content)
	}
	if err := service.Fetch(context.Background(), detail, charge); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || charges != 1 {
		t.Fatalf("calls=%d charges=%d", calls, charges)
	}
}

func TestArticleContentLeasesBackoffAndRefresh(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var sid, id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,category,language) VALUES ('https://8.8.8.8/feed','test','https://8.8.8.8/feed','test','en') RETURNING id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES ($1,'https://8.8.8.8/page','https://8.8.8.8/page','test','intro',NOW()) RETURNING id`, sid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	body := strings.Repeat("Full original paragraph. ", 30)
	started := make(chan struct{})
	proceed := make(chan struct{})
	calls := 0
	client := &http.Client{Transport: contentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		close(started)
		<-proceed
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("<article><p>" + body + "</p></article>")), Request: r}, nil
	})}
	svc := NewArticleContentService(db, client)
	intro := "intro"
	detail := func() *repository.ExploreArticleDetail {
		return &repository.ExploreArticleDetail{ID: id, URL: "https://8.8.8.8/page", Content: &intro}
	}
	first := detail()
	finished := make(chan error, 1)
	go func() { finished <- svc.Fetch(context.Background(), first, nil) }()
	<-started
	second := detail()
	if err := svc.Fetch(context.Background(), second, func() (func(), error) { t.Fatal("duplicate charged"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if second.ContentStatus != "fetching" {
		t.Fatalf("status %s", second.ContentStatus)
	}
	close(proceed)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE explore_articles SET content='new RSS intro' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	refreshed := detail()
	if err := svc.Overlay(context.Background(), refreshed); err != nil {
		t.Fatal(err)
	}
	if refreshed.ContentStatus != "fetched" || !strings.Contains(*refreshed.Content, "Full original paragraph.") {
		t.Fatalf("feed overwrote cache: %+v", refreshed)
	}
	// A failed fetch in cooldown returns the same body and never calls network.
	if _, err := db.Exec(`UPDATE explore_article_content SET status='failed',retry_at=NOW()+INTERVAL '1 hour',last_error='retry later' WHERE article_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	cooldown := detail()
	if err := svc.Fetch(context.Background(), cooldown, func() (func(), error) { t.Fatal("cooldown charged"); return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if cooldown.ContentStatus != "failed" || cooldown.ContentRetryAt == nil || calls != 1 {
		t.Fatalf("bad cooldown: %+v calls=%d", cooldown, calls)
	}
	// An expired owner cannot overwrite a newer lease's result.
	if _, err := db.Exec(`UPDATE explore_article_content SET status='fetching',lease_token='new-owner',lease_expires_at=NOW()-INTERVAL '1 minute' WHERE article_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	expired := detail()
	if err := svc.Overlay(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	if expired.ContentStatus != "failed" {
		t.Fatalf("expired=%+v", expired)
	}
	svc.client = &http.Client{Transport: contentTransport(func(r *http.Request) (*http.Response, error) {
		if _, err := db.Exec(`UPDATE explore_article_content SET lease_token='replacement',status='fetched',content='winner full complete content' WHERE article_id=$1`, id); err != nil {
			t.Error(err)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("<article><p>" + body + "</p></article>")), Request: r}, nil
	})}
	stale := detail()
	if err := svc.Fetch(context.Background(), stale, nil); err != nil {
		t.Fatal(err)
	}
	if *stale.Content != "winner full complete content" {
		t.Fatalf("stale lease overwrote winner %q", *stale.Content)
	}
}

func TestArticleContentFailureAndBudget(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var sid, id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,category,language) VALUES ('https://8.8.8.8/feed','test','https://8.8.8.8/feed','test','en') RETURNING id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES ($1,'https://8.8.8.8/page','https://8.8.8.8/page','test','intro',NOW()) RETURNING id`, sid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc := NewArticleContentService(db, &http.Client{Transport: contentTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`<article><p>Sign in to continue</p></article>`)), Request: r}, nil
	})})
	intro := "intro"
	d := &repository.ExploreArticleDetail{ID: id, URL: "https://8.8.8.8/page", Content: &intro}
	retryAt := time.Now().Add(6 * time.Hour)
	denied := &taskbudget.LimitError{Reason: "daily", Bucket: "fetch", RetryAt: &retryAt}
	if err := svc.Fetch(context.Background(), d, func() (func(), error) { return nil, denied }); !errors.Is(err, denied) {
		t.Fatalf("err=%v", err)
	}
	if d.ContentStatus != "feed" || d.ContentRetryAt != nil {
		t.Fatalf("personal budget blocked shared cache: %+v", d)
	}
	if calls != 0 || *d.Content != "intro" {
		t.Fatalf("denial fetched or lost intro: %+v", d)
	}

	if err := svc.Fetch(context.Background(), d, nil); err != nil {
		t.Fatal(err)
	}
	if d.ContentStatus != "failed" || d.ContentRetryAt == nil || *d.Content != "intro" || calls != 1 {
		t.Fatalf("failed fetch: %+v calls=%d", d, calls)
	}
}

func TestArticleContentCompletionRejectsChangedInputs(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"expired lease", `UPDATE explore_article_content SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE article_id=$1`},
		{"changed URL", `UPDATE explore_articles SET url='https://8.8.8.8/changed' WHERE id=$1`},
		{"grown feed", `UPDATE explore_articles SET content=repeat('new longer feed body ',100) WHERE id=$1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, done := testdb.New(t)
			defer done()
			var sid, id int
			if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,category,language) VALUES ('https://8.8.8.8/feed','test','https://8.8.8.8/feed','test','en') RETURNING id`).Scan(&sid); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES ($1,'https://8.8.8.8/page','https://8.8.8.8/page','test','intro',NOW()) RETURNING id`, sid).Scan(&id); err != nil {
				t.Fatal(err)
			}
			svc := NewArticleContentService(db, &http.Client{Transport: contentTransport(func(r *http.Request) (*http.Response, error) {
				if _, err := db.Exec(tc.mutation, id); err != nil {
					t.Error(err)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("<article><p>" + strings.Repeat("Original full body. ", 30) + "</p></article>")), Request: r}, nil
			})})
			intro := "intro"
			d := &repository.ExploreArticleDetail{ID: id, URL: "https://8.8.8.8/page", Content: &intro}
			if err := svc.Fetch(context.Background(), d, nil); err != nil {
				t.Fatal(err)
			}
			var body, status string
			if err := db.QueryRow(`SELECT content,status FROM explore_article_content WHERE article_id=$1`, id).Scan(&body, &status); err != nil {
				t.Fatal(err)
			}
			if body != "" || status == "fetched" {
				t.Fatalf("stale completion published: %s %q", status, body)
			}
		})
	}
}

func TestCachedContentDoesNotHideGrownFeed(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var sid, id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,category,language) VALUES ('https://8.8.8.8/feed','test','https://8.8.8.8/feed','test','en') RETURNING id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES ($1,'https://8.8.8.8/page','https://8.8.8.8/page','test','intro',NOW()) RETURNING id`, sid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_article_content(article_id,source_url,content,status,lease_token) VALUES ($1,'https://8.8.8.8/page','short cached body','fetched','old')`, id); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("new longer feed body ", 100)
	d := &repository.ExploreArticleDetail{ID: id, URL: "https://8.8.8.8/page", Content: &full}
	svc := NewArticleContentService(db, nil)
	if err := svc.Overlay(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if *d.Content != full || d.ContentStatus != "feed" {
		t.Fatalf("cache hid currentfeed: %+v", d)
	}
}

func TestStaleDetailBeforeClaimKeepsCurrentFeed(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	var sid, id int
	if err := db.QueryRow(`INSERT INTO recommended_feeds(url,title,normalized_url,category,language) VALUES ('https://8.8.8.8/feed','test','https://8.8.8.8/feed','test','en') RETURNING id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	full := strings.Repeat("Newly refreshed full feed text. ", 100)
	if err := db.QueryRow(`INSERT INTO explore_articles(source_id,url,normalized_url,title,content,fetched_at) VALUES ($1,'https://8.8.8.8/page','https://8.8.8.8/page','test',$2,NOW()) RETURNING id`, sid, full).Scan(&id); err != nil {
		t.Fatal(err)
	}
	svc := NewArticleContentService(db, &http.Client{Transport: contentTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`<article><p>` + strings.Repeat("Shorter original body. ", 20) + `</p></article>`)), Request: r}, nil
	})})
	stale := "stale old RSS intro"
	d := &repository.ExploreArticleDetail{ID: id, URL: "https://8.8.8.8/page", Content: &stale}
	if err := svc.Fetch(context.Background(), d, nil); err != nil {
		t.Fatal(err)
	}
	if d.Content == nil || *d.Content != full || d.ContentStatus != "failed" {
		t.Fatalf("regressed refreshed feed: %+v", d)
	}
	var cached string
	if err := db.QueryRow(`SELECT content FROM explore_article_content WHERE article_id=$1`, id).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if cached != "" {
		t.Fatal("shorter body published")
	}
}
