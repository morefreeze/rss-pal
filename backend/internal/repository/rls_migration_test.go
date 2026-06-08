package repository_test

import (
	"sort"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

// TestMigration033_EnablesRLS verifies every table named in migration 033
// has rowsecurity enabled and at least one policy.
func TestMigration033_EnablesRLS(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	expected := []string{
		// private
		"reading_progress", "playback_progress", "user_preferences",
		"user_tags", "article_user_tags", "tag_suggestion_dismissals",
		"interest_topics", "interest_tags", "interest_categories",
		"user_insights", "article_events", "weekly_digests",
		"daily_digests", "hidden_articles", "user_ai_configs",
		// shared-owned
		"feeds", "articles", "summary_templates",
	}
	sort.Strings(expected)

	for _, name := range expected {
		var enabled bool
		if err := db.QueryRow(`
            SELECT relrowsecurity
              FROM pg_class
             WHERE relname = $1
               AND relkind = 'r'
        `, name).Scan(&enabled); err != nil {
			t.Errorf("%s: lookup: %v", name, err)
			continue
		}
		if !enabled {
			t.Errorf("%s: rowsecurity not enabled", name)
		}

		var policyCount int
		if err := db.QueryRow(`
            SELECT COUNT(*) FROM pg_policies WHERE tablename = $1
        `, name).Scan(&policyCount); err != nil {
			t.Errorf("%s: policy lookup: %v", name, err)
			continue
		}
		if policyCount == 0 {
			t.Errorf("%s: no policies attached", name)
		}
	}
}

// TestMigration033_HelpersExist verifies the two helper functions are present.
func TestMigration033_HelpersExist(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	for _, fn := range []string{"app_current_user_id", "app_rls_bypass"} {
		var ok bool
		if err := db.QueryRow(`
            SELECT EXISTS (
                SELECT 1 FROM pg_proc WHERE proname = $1
            )
        `, fn).Scan(&ok); err != nil {
			t.Errorf("%s: %v", fn, err)
			continue
		}
		if !ok {
			t.Errorf("%s: function not created", fn)
		}
	}
}
