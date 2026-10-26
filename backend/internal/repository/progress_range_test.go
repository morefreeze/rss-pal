package repository_test

import (
	"errors"
	"fmt"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
)

func TestMarkReadRangeIncludesUnloadedAndReadAnchor(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO users(id,username,password_hash) VALUES(1,'reader','x'),(2,'other','x')`)
	must(`INSERT INTO feeds(id,url,title,owner_id) VALUES(1,'https://one','one',1),(2,'https://two','two',2)`)
	for i := 1; i <= 55; i++ {
		must(`INSERT INTO articles(id,feed_id,title,url,published_at,fetched_at) VALUES($1,1,$2,$2,'2026-10-26','2026-10-26')`, i, fmt.Sprint(i))
	}
	must(`INSERT INTO articles(id,feed_id,title,url,published_at,fetched_at) VALUES(99,2,'other','other','2026-10-26','2026-10-26')`)
	repo := repository.NewProgressRepository(db)
	for _, sort := range []repository.SortMode{repository.SortPublished, repository.SortCaptured} {
		for _, dir := range []repository.SortDir{repository.SortAsc, repository.SortDesc} {
			for _, direction := range []string{"above", "below"} {
				must(`DELETE FROM reading_progress`)
				must(`INSERT INTO reading_progress(user_id,article_id,is_completed) VALUES(1,30,true),(1,55,NULL)`)
				q := repository.ReadRange{Filter: repository.ArticleFilter{UserID: 1, UnreadOnly: true}, AnchorID: 30, Direction: direction, Sort: sort, Dir: dir}
				ids, err := repo.MarkReadRange(q)
				if err != nil {
					t.Fatal(err)
				}
				higher := (direction == "above") == (dir == repository.SortDesc)
				want := 29
				if higher {
					want = 25
				}
				if len(ids) != want {
					t.Fatalf("sort=%v dir=%v %s changed %d want %d", sort, dir, direction, len(ids), want)
				}
				var count int
				if err := db.QueryRow(`SELECT count(*) FROM reading_progress WHERE user_id=1 AND is_completed`).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != want+1 {
					t.Fatalf("count=%d", count)
				}
				for _, id := range ids {
					if id == 99 || (higher && id < 30) || (!higher && id > 30) {
						t.Fatalf("out of range %d", id)
					}
				}
			}
		}
	}
	_, err := repo.MarkReadRange(repository.ReadRange{Filter: repository.ArticleFilter{UserID: 1}, AnchorID: 99, Direction: "below"})
	if !errors.Is(err, repository.ErrReadAnchorNotFound) {
		t.Fatalf("foreign anchor: %v", err)
	}
}

func TestMarkReadRangeMatchesListOrderingAndFilters(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	must(`INSERT INTO users(id,username,password_hash) VALUES(1,'reader','x'),(2,'other','x')`)
	must(`INSERT INTO feeds(id,url,title,owner_id,feed_type) VALUES(1,'https://one','one',1,'clip'),(2,'https://two','two',1,'rss'),(3,'https://foreign','foreign',2,'clip')`)
	must(`INSERT INTO user_tags(id,user_id,name) VALUES(1,1,'target'),(2,2,'other')`)
	for i := 1; i <= 60; i++ {
		feed := 1
		if i%7 == 0 {
			feed = 2
		}
		if i%11 == 0 {
			feed = 3
		}
		// Invert capture/publication dates and include NULL publication dates.
		must(`INSERT INTO articles(id,feed_id,title,url,published_at,fetched_at) VALUES($1,$2,$3,$4,CASE WHEN $1::int%3=0 THEN NULL ELSE timestamp '2026-09-01'+$1::int*interval '1 day' END,timestamp '2026-10-26'-$1::int*interval '6 hours')`, i, feed, fmt.Sprint(i), fmt.Sprintf("https://host%d.example/%d", i%2, i))
		if i%2 == 0 {
			must(`INSERT INTO user_preferences(user_id,article_id,signal_type,signal_value) VALUES(1,$1,'save',1)`, i)
		}
		if i%3 == 0 {
			must(`INSERT INTO article_user_tags(user_id,article_id,tag_id) VALUES(1,$1,1)`, i)
		}
	}
	must(`INSERT INTO hidden_articles(user_id,article_id) VALUES(1,60)`)
	feed, tag := 1, 1
	filters := []repository.ArticleFilter{
		{UserID: 1}, {UserID: 1, FeedID: &feed, SavedOnly: true}, {UserID: 1, TagID: &tag}, {UserID: 1, Untagged: true},
	}
	for _, filter := range filters {
		for _, sort := range []repository.SortMode{repository.SortPublished, repository.SortCaptured} {
			for _, dir := range []repository.SortDir{repository.SortAsc, repository.SortDesc} {
				must(`DELETE FROM reading_progress`)
				list, err := repository.NewArticleRepository(db).GetAll(100, 0, filter.FeedID, false, filter.SavedOnly, 1, filter.TagID, filter.Untagged, sort, dir)
				if err != nil {
					t.Fatal(err)
				}
				middle := len(list) / 2
				for _, direction := range []string{"above", "below"} {
					must(`DELETE FROM reading_progress`)
					ids, err := repository.NewProgressRepository(db).MarkReadRange(repository.ReadRange{Filter: filter, AnchorID: list[middle].ID, Direction: direction, Sort: sort, Dir: dir})
					if err != nil {
						t.Fatal(err)
					}
					want := list[:middle+1]
					if direction == "below" {
						want = list[middle:]
					}
					got := map[int]bool{}
					for _, id := range ids {
						got[id] = true
					}
					if len(got) != len(want) {
						t.Fatalf("filter=%+v sort=%v dir=%v %s: got %v want %d", filter, sort, dir, direction, ids, len(want))
					}
					for _, a := range want {
						if !got[a.ID] {
							t.Fatalf("missing %d", a.ID)
						}
					}
				}
			}
		}
	}
	must(`DELETE FROM reading_progress`)
	ids, err := repository.NewProgressRepository(db).MarkReadRange(repository.ReadRange{Filter: repository.ArticleFilter{UserID: 1}, ClipOnly: true, Source: "host:host0.example", AnchorID: 2, Direction: "below", Sort: repository.SortCaptured})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id%2 != 0 || id%7 == 0 || id%11 == 0 || id == 60 {
			t.Fatalf("clip source scope leaked: %d", id)
		}
	}
	if len(ids) < 10 {
		t.Fatalf("too few matching clips %v", ids)
	}
	for _, q := range []repository.ReadRange{
		{Filter: repository.ArticleFilter{UserID: 1}, AnchorID: 60, Direction: "below"},
		{Filter: repository.ArticleFilter{UserID: 1, TagID: &tag}, AnchorID: 2, Direction: "below"},
		{Filter: repository.ArticleFilter{UserID: 1}, ClipOnly: true, AnchorID: 7, Direction: "below"},
	} {
		if _, err := repository.NewProgressRepository(db).MarkReadRange(q); !errors.Is(err, repository.ErrReadAnchorNotFound) {
			t.Fatalf("invalid anchor: %v", err)
		}
	}
}
