package repository_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/explore"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestRedditTopSeedsLoadedAndLegacyDisabled(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "051_explore_content_version.sql")
	defer cleanup()
	// The later transport migration adds this column; exercise old seed setup
	// with the new repository schema expectation without switching transport yet.
	if _, err := db.Exec(`ALTER TABLE explore_registry_providers ADD COLUMN browser_only BOOLEAN NOT NULL DEFAULT false`); err != nil {
		t.Fatal(err)
	}
	providers, err := repository.NewExploreRegistryRepository(db).LoadDueProviders(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, p := range providers {
		if p.Key == "reddit-programming" {
			t.Fatal("obsolete unscored provider enabled")
		}
		if p.Kind != "reddit_top" {
			continue
		}
		count++
		if !strings.HasSuffix(p.Endpoint, "#min_score=100") || p.SyncInterval != 6*time.Hour {
			t.Fatalf("bad seed: %+v", p)
		}
		adapter, ok := explore.DefaultProviderAdapters()[p.Kind]
		if !ok {
			t.Fatal("missing adapter")
		}
		if _, err := adapter.Parse(explore.Provider{Endpoint: p.Endpoint}, []byte(`{"kind":"Listing","data":{"children":[]}}`)); err != nil {
			t.Fatal(err)
		}
	}
	if count != 8 {
		t.Fatalf("scored seeds=%d, want 8", count)
	}
	// Deployments rerun the migration chain. Preserve operator settings and
	// conditional-fetch state, and do not duplicate registered seeds.
	if _, err := db.Exec(`UPDATE explore_registry_providers SET endpoint=replace(endpoint,'min_score=100','min_score=250'), enabled=false, etag='keep' WHERE provider_key='reddit-programming-top-week'`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/050_reddit_top_seeds.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	var endpoint, etag string
	var enabled bool
	if err := db.QueryRow(`SELECT endpoint,enabled,etag FROM explore_registry_providers WHERE provider_key='reddit-programming-top-week'`).Scan(&endpoint, &enabled, &etag); err != nil {
		t.Fatal(err)
	}
	if enabled || etag != "keep" || !strings.HasSuffix(endpoint, "#min_score=250") {
		t.Fatalf("migration overwrote runtime settings: %s %t %s", endpoint, enabled, etag)
	}
}
