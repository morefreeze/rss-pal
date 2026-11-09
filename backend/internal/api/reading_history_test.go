package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/bytedance/rss-pal/internal/repository"
)

func TestReadingHistoryHTTP(t *testing.T) {
	f, cleanup := newHTTPLeakFixture(t)
	defer cleanup()
	handler := api.NewReadingHistoryHandler(repository.NewReadingHistoryRepository(f.appDB))
	auth := api.NewAuthHandler(f.cfg, repository.NewUserRepository(f.appDB), repository.NewRefreshTokenRepository(f.appDB))
	group := f.router.Group("/api/reading-history", auth.AuthMiddleware(), api.RLSTxMiddleware(f.appDB))
	group.GET("", handler.List)
	group.POST("/:kind/:id", handler.Record)

	var clipID, exploreID int
	if err := f.privDB.QueryRow(`
		WITH feed AS (
			INSERT INTO feeds(url,title,owner_id,feed_type) VALUES ('clip:test','网摘',$1,'clip') RETURNING id
		)
		INSERT INTO articles(feed_id,title,url) SELECT id,'网摘文章','https://clip.example/a' FROM feed RETURNING id
	`, f.userA).Scan(&clipID); err != nil {
		t.Fatal(err)
	}
	if err := f.privDB.QueryRow(`
		WITH source AS (
			INSERT INTO recommended_feeds(url,normalized_url,title,category,language) VALUES ('http://a','http://a','探索来源','test','en') RETURNING id
		)
		INSERT INTO explore_articles(source_id,url,normalized_url,title)
		SELECT id,'https://explore.example/a','https://explore.example/a','探索文章' FROM source RETURNING id
	`).Scan(&exploreID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.privDB.Exec(`
		INSERT INTO article_events(user_id,article_id,event_type) VALUES ($1,$2,'click');
	`, f.userA, f.articleA); err != nil {
		t.Fatal(err)
	}

	const path = "/api/reading-history"
	list := func(userID int, query string) ([]string, string) {
		t.Helper()
		w := f.do(t, http.MethodGet, path+query, userID)
		if w.Code != http.StatusOK {
			t.Fatalf("list: %d %s", w.Code, w.Body)
		}
		var page struct {
			Items []struct {
				ArticleID int    `json:"article_id"`
				Kind      string `json:"kind"`
				Title     string `json:"title"`
			} `json:"items"`
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Items == nil {
			t.Fatalf("decode list: %v %s", err, w.Body)
		}
		var items []string
		for _, item := range page.Items {
			items = append(items, fmt.Sprintf("%s:%d:%s", item.Kind, item.ArticleID, item.Title))
		}
		return items, page.NextCursor
	}
	record := func(userID int, kind string, articleID, wantStatus int) {
		t.Helper()
		w := f.do(t, http.MethodPost, fmt.Sprintf("%s/%s/%d", path, kind, articleID), userID)
		if w.Code != wantStatus {
			t.Fatalf("record: got %d, want %d: %s", w.Code, wantStatus, w.Body)
		}
	}

	if items, _ := list(f.userA, ""); len(items) != 0 {
		t.Fatalf("legacy clicks imported: %v", items)
	}
	record(f.userA, "article", f.articleA, http.StatusNoContent)
	record(f.userA, "article", clipID, http.StatusNoContent)
	record(f.userA, "explore", exploreID, http.StatusNoContent)
	record(f.userA, "article", f.articleA, http.StatusNoContent)
	record(f.userB, "article", f.articleA, http.StatusNotFound)
	record(f.userB, "explore", exploreID, http.StatusNotFound)

	want := []string{
		fmt.Sprintf("article:%d:a1", f.articleA),
		fmt.Sprintf("explore:%d:探索文章", exploreID),
		fmt.Sprintf("clip:%d:网摘文章", clipID),
	}
	items, cursor := list(f.userA, "?limit=2")
	if fmt.Sprint(items) != fmt.Sprint(want[:2]) || cursor == "" {
		t.Fatalf("first page = %v cursor %q, want %v and a cursor", items, cursor, want[:2])
	}
	items, cursor = list(f.userA, "?limit=2&before="+url.QueryEscape(cursor))
	if fmt.Sprint(items) != fmt.Sprint(want[2:]) || cursor != "" {
		t.Fatalf("second page = %v cursor %q, want %v and no cursor", items, cursor, want[2:])
	}
	if _, err := f.privDB.Exec(`DELETE FROM explore_articles WHERE id=$1`, exploreID); err != nil {
		t.Fatal(err)
	}
	archived := []string{want[0], "explore:0:探索文章", want[2]}
	if items, _ := list(f.userA, ""); fmt.Sprint(items) != fmt.Sprint(archived) {
		t.Fatalf("history after Explore cache eviction = %v, want %v", items, archived)
	}
	w := f.do(t, http.MethodGet, path, f.userA)
	var availability struct {
		Items []struct{ Available bool }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &availability); err != nil || len(availability.Items) != 3 ||
		!availability.Items[0].Available || availability.Items[1].Available || !availability.Items[2].Available {
		t.Fatalf("evicted article must be marked unavailable: %v %s", err, w.Body)
	}
	if err := f.privDB.QueryRow(`
		INSERT INTO explore_articles(source_id,url,normalized_url,title)
		SELECT id,'https://explore.example/a','https://explore.example/a','探索文章' FROM recommended_feeds
		WHERE normalized_url='http://a' RETURNING id
	`).Scan(&exploreID); err != nil {
		t.Fatal(err)
	}
	record(f.userA, "explore", exploreID, http.StatusNoContent)
	reopened := []string{fmt.Sprintf("explore:%d:探索文章", exploreID), want[0], want[2]}
	if items, _ := list(f.userA, ""); fmt.Sprint(items) != fmt.Sprint(reopened) {
		t.Fatalf("reopening a refreshed Explore article = %v, want %v", items, reopened)
	}
	// Model a newer open that won the row lock before this older write.
	if _, err := f.privDB.Exec(`
		UPDATE reading_history SET opened_at=NOW()+INTERVAL '1 hour'
		WHERE user_id=$1 AND explore_article_id=$2
	`, f.userA, exploreID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.privDB.Exec(`
		INSERT INTO feeds(url,title,owner_id) VALUES ('https://b.example/feed','第二订阅',$1)
	`, f.userA); err != nil {
		t.Fatal(err)
	}
	var duplicateID int
	if err := f.privDB.QueryRow(`
		WITH source AS (
			INSERT INTO recommended_feeds(url,normalized_url,title,category,language)
			VALUES ('https://b.example/feed','https://b.example/feed','第二来源','test','en') RETURNING id
		)
		INSERT INTO explore_articles(source_id,url,normalized_url,title)
		SELECT id,'https://explore.example/a','https://explore.example/a','旧打开' FROM source RETURNING id
	`).Scan(&duplicateID); err != nil {
		t.Fatal(err)
	}
	record(f.userA, "explore", duplicateID, http.StatusNoContent)
	if items, _ := list(f.userA, ""); fmt.Sprint(items) != fmt.Sprint(reopened) {
		t.Fatalf("older write overwrote the latest Explore snapshot: %v", items)
	}
	if items, _ := list(f.userB, ""); len(items) != 0 {
		t.Fatalf("another account sees history: %v", items)
	}
}
