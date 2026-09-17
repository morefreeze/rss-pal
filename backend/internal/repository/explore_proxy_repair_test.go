package repository_test

import (
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"os"
	"testing"
)

func TestProxyURLRepairSeparatesDomainsWithoutMovingCachedArticles(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	var ip, first, second int
	for i, raw := range []string{"https://93.184.216.34:443/feed", "https://first.example/feed", "https://second.example/feed"} {
		var id int
		if err := db.QueryRow(`INSERT INTO recommended_feeds(url,normalized_url,title,category,language,validation_status) VALUES($1,$1,'fixture','test','en','valid') RETURNING id`, raw).Scan(&id); err != nil {
			t.Fatal(err)
		}
		switch i {
		case 0:
			ip = id
		case 1:
			first = id
		case 2:
			second = id
		}
	}
	if _, err := db.Exec(`UPDATE recommended_feeds SET merged_into_source_id=$1 WHERE id IN($2,$3)`, ip, first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_source_observations(provider_id,source_id,external_key) SELECT id,$1,'https://first.example/feed' FROM explore_registry_providers WHERE provider_key='chinese-independent'`, ip); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO explore_articles(source_id,url,normalized_url,title) VALUES($1,'https://first.example/post','https://first.example/post','cached')`, ip); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../../docs/operations/repair-explore-proxy-urls.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	var invalid, restored, queued, cached int
	if err := db.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE id=$1 AND validation_status='invalid' AND is_broken`, ip).Scan(&invalid); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM recommended_feeds WHERE id IN($1,$2) AND validation_status='pending' AND merged_into_source_id IS NULL AND last_fetched_at IS NULL`, first, second).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_fetch_queue WHERE source_id IN($1,$2) AND status='pending' AND priority=1000`, first, second).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM explore_articles WHERE source_id=$1`, ip).Scan(&cached); err != nil {
		t.Fatal(err)
	}
	if invalid != 1 || restored != 2 || queued != 2 || cached != 1 {
		t.Fatalf("invalid=%d restored=%d queued=%d cache=%d", invalid, restored, queued, cached)
	}
}
