package main

import (
	"context"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestBackfillScopeAndAtomicDateOnlyUpdate(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var owner, other, feed, otherFeed, normalFeed int
	for _, row := range []struct {
		name string
		id   *int
	}{{"date-owner", &owner}, {"other", &other}} {
		if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'unused') RETURNING id`, row.name).Scan(row.id); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		url  string
		user int
		id   *int
	}{{"bookmarklet://user/1", owner, &feed}, {"bookmarklet://user/2", other, &otherFeed}, {"https://example.com/rss", owner, &normalFeed}} {
		if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES($1,'source',$2) RETURNING id`, row.url, row.user).Scan(row.id); err != nil {
			t.Fatal(err)
		}
	}
	since := time.Date(2026, 9, 13, 16, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 1, 0)
	var target int
	for _, row := range []struct {
		feed      int
		fetched   time.Time
		published any
		clip      bool
	}{{feed, since, nil, true}, {feed, since.Add(-time.Second), nil, true}, {otherFeed, since, nil, true}, {normalFeed, since, nil, false}, {feed, since, "2020-01-01", true}, {feed, until, nil, true}} {
		var id int
		if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,is_clip,fetched_at,published_at) VALUES($1,'article',$2,'unchanged content',$3,$4,$5) RETURNING id`, row.feed, "https://example.com/"+row.fetched.Format(time.RFC3339)+time.Now().Format(".000000000"), row.clip, row.fetched, row.published).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if target == 0 {
			target = id
		}
	}
	if _, err := db.Exec(`INSERT INTO article_shares(public_id,article_id,created_by,snapshot) VALUES('date-share',$1,$2,'{"title":"snapshot title","content":"snapshot content","snapshotted_at":"2026-09-14T00:00:00Z"}'),('has-date',$1,$2,'{"published_at":"2001-01-01T00:00:00Z"}')`, target, owner); err != nil {
		t.Fatal(err)
	}
	scope := repairScope{Owner: owner, Since: since, Until: until}
	rows, err := selectCandidates(context.Background(), db, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != target {
		t.Fatalf("selected %+v", rows)
	}
	date := time.Date(2023, 6, 23, 0, 0, 0, 0, time.UTC)
	for _, wrong := range []repairScope{{Owner: other, Since: since, Until: until}, {Owner: owner, Since: since.Add(time.Second), Until: until}} {
		changed, shares, err := applyDate(context.Background(), db, wrong, rows[0], date)
		if err != nil || changed || shares != 0 {
			t.Fatalf("scope recheck failed: %v %d %v", changed, shares, err)
		}
	}
	if _, err := db.Exec(`ALTER TABLE article_shares ADD CONSTRAINT reject_date_test CHECK (snapshot->>'published_at' IS DISTINCT FROM '2023-06-23T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := applyDate(context.Background(), db, scope, rows[0], date); err == nil {
		t.Fatal("expected snapshot write failure")
	}
	var stillNull bool
	if err := db.QueryRow(`SELECT published_at IS NULL FROM articles WHERE id=$1`, target).Scan(&stillNull); err != nil || !stillNull {
		t.Fatalf("article not rolled back: %v %v", stillNull, err)
	}
	if _, err := db.Exec(`ALTER TABLE article_shares DROP CONSTRAINT reject_date_test`); err != nil {
		t.Fatal(err)
	}
	changed, shares, err := applyDate(context.Background(), db, scope, rows[0], date)
	if err != nil || !changed || shares != 1 {
		t.Fatalf("changed %v shares %d err %v", changed, shares, err)
	}
	var content, snapshotContent, title, shareDate, originalDate string
	if err := db.QueryRow(`SELECT a.content,s.snapshot->>'content',s.snapshot->>'title',s.snapshot->>'published_at' FROM articles a JOIN article_shares s ON s.article_id=a.id WHERE s.public_id='date-share'`).Scan(&content, &snapshotContent, &title, &shareDate); err != nil {
		t.Fatal(err)
	}
	if content != "unchanged content" || snapshotContent != "snapshot content" || title != "snapshot title" || shareDate != date.Format(time.RFC3339) {
		t.Fatalf("unexpected content/snapshot: %q %q %q %q", content, snapshotContent, title, shareDate)
	}
	if err := db.QueryRow(`SELECT snapshot->>'published_at' FROM article_shares WHERE public_id='has-date'`).Scan(&originalDate); err != nil {
		t.Fatal(err)
	}
	if originalDate != "2001-01-01T00:00:00Z" {
		t.Fatal("overwrote existing snapshot date")
	}
	changed, shares, err = applyDate(context.Background(), db, scope, rows[0], date.AddDate(1, 0, 0))
	if err != nil || changed || shares != 0 {
		t.Fatalf("not idempotent: %v %d %v", changed, shares, err)
	}
	rows, err = selectCandidates(context.Background(), db, scope)
	if err != nil || len(rows) != 0 {
		t.Fatalf("remaining %+v %v", rows, err)
	}
}
