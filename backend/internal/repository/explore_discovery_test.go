package repository

import (
	"fmt"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
	"time"
)

func TestExploreDiscoveryChannelsAndFiltering(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	user, _ := insertExploreUsers(t, db)
	a := insertExploreSource(t, db, "https://multi.example/feed", "Multi")
	b := insertExploreSource(t, db, "https://directory.example/feed", "Directory")
	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 3; i++ {
		insertExploreArticle(t, db, a, now.Add(-time.Duration(i)*time.Minute), "multi")
	}
	insertExploreArticle(t, db, b, now, "directory")
	for _, key := range []string{"reddit-programming-top-week", "reddit-programming-top-month", "chinese-independent"} {
		if _, err := db.Exec(`INSERT INTO explore_source_observations(provider_id,source_id,external_key,last_seen_at) SELECT id,$1,'post',NOW() FROM explore_registry_providers WHERE provider_key=$2`, a, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations(provider_id,source_id,external_key,last_seen_at) SELECT id,$1,'directory',NOW() FROM explore_registry_providers WHERE provider_key='chinese-independent'`, b); err != nil {
		t.Fatal(err)
	}
	repo := NewExploreRepository(db)
	// Both cold and snapshot paths must expose the same provenance and filter before slicing.
	for _, snapshot := range []bool{false, true} {
		if snapshot {
			insertExploreDoneBatch(t, db, user, now, []exploreTestBatchSource{{sourceID: a, rank: 1, topic: "programming"}, {sourceID: b, rank: 2, topic: "technology"}})
		}
		first, err := repo.GetPage(user, ExploreListParams{Channel: "reddit:programming", Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Articles) != 2 || !first.HasMore {
			t.Fatalf("snapshot=%v page=%+v", snapshot, first)
		}
		for _, item := range first.Articles {
			if item.SourceID != a || len(item.DiscoveryChannels) != 2 {
				t.Fatalf("channels=%+v", item)
			}
			found := false
			for _, c := range item.DiscoveryChannels {
				if c.Key == "reddit:programming" && c.Label == "Reddit · r/programming" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing subreddit: %+v", item.DiscoveryChannels)
			}
		}
		second, err := repo.GetPage(user, ExploreListParams{Channel: "reddit", Offset: 2, Limit: 2})
		if err != nil || len(second.Articles) != 1 || second.HasMore {
			t.Fatalf("second=%+v err=%v", second, err)
		}
		empty, err := repo.GetPage(user, ExploreListParams{Channel: "reddit:mindustry"})
		if err != nil || len(empty.Articles) != 0 || len(empty.DiscoveryChannels) == 0 {
			t.Fatalf("empty=%+v err=%v", empty, err)
		}
		if snapshot {
			intersection, err := repo.GetPage(user, ExploreListParams{Channel: "reddit", Topic: "technology"})
			if err != nil || len(intersection.Articles) != 0 {
				t.Fatalf("intersection=%+v err=%v", intersection, err)
			}
		}
	}
	article := insertExploreArticle(t, db, a, now, "saved")
	if _, err := db.Exec(`INSERT INTO explore_article_states(user_id,normalized_url,saved,saved_at) SELECT $1,normalized_url,true,NOW() FROM explore_articles WHERE id=$2`, user, article); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.GetPage(user, ExploreListParams{View: "later", Channel: "reddit"})
	if err != nil || len(saved.Articles) != 1 || len(saved.Articles[0].DiscoveryChannels) != 2 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	// Exercise the SQL predicate with a literal source id as used by the saved query.
	var matched bool
	if err := db.QueryRow("SELECT "+exploreDiscoverySQLFilter(fmt.Sprint(a), "$1"), "reddit:programming").Scan(&matched); err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("SQL must match exact subreddit before saved pagination")
	}
	saved, err = repo.GetPage(user, ExploreListParams{View: "later", Channel: "reddit:programming"})
	if err != nil || len(saved.Articles) != 1 {
		t.Fatalf("saved subreddit=%+v err=%v", saved, err)
	}
	saved, err = repo.GetPage(user, ExploreListParams{View: "later", Channel: "reddit:mindustry"})
	if err != nil || len(saved.Articles) != 0 {
		t.Fatalf("saved filtered=%+v err=%v", saved, err)
	}
}
