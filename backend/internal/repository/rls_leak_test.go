package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

// rlsFixture stages two users with distinct private feeds (owner_id set)
// plus one shared feed (owner_id NULL) and one article in each feed.
// privDB bypasses RLS (used to seed cross-user data); appDB is bound to
// the rsspal_app role and is subject to migration 033's policies.
type rlsFixture struct {
	privDB                                 *sql.DB
	appDB                                  *sql.DB
	userA, userB                           int
	privateFeedA, privateFeedB, sharedFeed int
	articleA, articleB, articleShared      int
}

func newRLSFixture(t *testing.T) (*rlsFixture, func()) {
	t.Helper()
	privDB, schema, cleanupSchema := testdb.NewWithSchema(t)
	appDB, cleanupApp := testdb.NewAsApp(t, schema)
	f := &rlsFixture{privDB: privDB, appDB: appDB}

	if err := privDB.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('a', 'x') RETURNING id`).Scan(&f.userA); err != nil {
		t.Fatalf("seed userA: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('b', 'y') RETURNING id`).Scan(&f.userB); err != nil {
		t.Fatalf("seed userB: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO feeds (url, title, owner_id) VALUES ('http://a', 'A', $1) RETURNING id`, f.userA).Scan(&f.privateFeedA); err != nil {
		t.Fatalf("seed feedA: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO feeds (url, title, owner_id) VALUES ('http://b', 'B', $1) RETURNING id`, f.userB).Scan(&f.privateFeedB); err != nil {
		t.Fatalf("seed feedB: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO feeds (url, title) VALUES ('http://s', 'S') RETURNING id`).Scan(&f.sharedFeed); err != nil {
		t.Fatalf("seed sharedFeed: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 'a1', 'http://a/1', NOW()) RETURNING id`, f.privateFeedA).Scan(&f.articleA); err != nil {
		t.Fatalf("seed articleA: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 'b1', 'http://b/1', NOW()) RETURNING id`, f.privateFeedB).Scan(&f.articleB); err != nil {
		t.Fatalf("seed articleB: %v", err)
	}
	if err := privDB.QueryRow(`INSERT INTO articles (feed_id, title, url, published_at) VALUES ($1, 's1', 'http://s/1', NOW()) RETURNING id`, f.sharedFeed).Scan(&f.articleShared); err != nil {
		t.Fatalf("seed articleShared: %v", err)
	}
	return f, func() { cleanupApp(); cleanupSchema() }
}

// asUser runs fn inside a tx on appDB with app.user_id = uid set via
// set_config(..., true) — i.e. SET LOCAL, scoped to the tx. The tx is
// rolled back on return so each test stays hermetic.
func (f *rlsFixture) asUser(t *testing.T, uid int, fn func(*sql.Tx)) {
	t.Helper()
	tx, err := f.appDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT set_config('app.user_id', $1, true)`, uid); err != nil {
		t.Fatalf("set_config: %v", err)
	}
	fn(tx)
}

// seedUserScopedRow inserts a row using the privileged DB. Failure is fatal
// because the matrix tests cannot prove isolation if the seed itself was
// rejected.
func seedUserScopedRow(t *testing.T, privDB *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := privDB.Exec(query, args...); err != nil {
		t.Fatalf("seed row: %v\nSQL: %s", err, query)
	}
}

// ----------------------------------------------------------------------------
// feeds + articles: shared-but-owned matrix.
// ----------------------------------------------------------------------------

func TestRLS_Feeds_ScopedByOwner(t *testing.T) {
	f, cleanup := newRLSFixture(t)
	defer cleanup()

	f.asUser(t, f.userA, func(tx *sql.Tx) {
		rows, err := tx.Query(`SELECT id FROM feeds ORDER BY id`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		defer rows.Close()
		seen := map[int]bool{}
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				t.Fatalf("scan: %v", err)
			}
			seen[id] = true
		}
		if seen[f.privateFeedB] {
			t.Errorf("userA leaked privateFeedB (id=%d)", f.privateFeedB)
		}
		if !seen[f.privateFeedA] {
			t.Errorf("userA missing own privateFeedA (id=%d): seen=%v", f.privateFeedA, seen)
		}
		if !seen[f.sharedFeed] {
			t.Errorf("userA missing shared feed (id=%d): seen=%v", f.sharedFeed, seen)
		}
	})
}

func TestRLS_Articles_ScopedByFeedOwner(t *testing.T) {
	f, cleanup := newRLSFixture(t)
	defer cleanup()

	f.asUser(t, f.userB, func(tx *sql.Tx) {
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM articles WHERE id = $1`, f.articleA).Scan(&n); err != nil {
			t.Fatalf("count articleA: %v", err)
		}
		if n != 0 {
			t.Errorf("userB sees userA's private article (count=%d)", n)
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM articles WHERE id = $1`, f.articleShared).Scan(&n); err != nil {
			t.Fatalf("count articleShared: %v", err)
		}
		if n != 1 {
			t.Errorf("userB cannot see shared article (count=%d)", n)
		}
		if err := tx.QueryRow(`SELECT COUNT(*) FROM articles WHERE id = $1`, f.articleB).Scan(&n); err != nil {
			t.Fatalf("count articleB: %v", err)
		}
		if n != 1 {
			t.Errorf("userB cannot see own articleB (count=%d)", n)
		}
	})
}

// ----------------------------------------------------------------------------
// Private tables: scoped by user_id.
//
// For each table the seed inserts one row each for userA + userB via the
// privileged DB, then asserts the appDB tx bound to userA sees exactly the
// single A row. $1 = an article id (used by article-FK tables to vary the
// row across users) and $2 = the user id.
// ----------------------------------------------------------------------------

func TestRLS_PrivateTablesAreScoped(t *testing.T) {
	cases := []struct {
		name     string
		seedSQL  string
		countSQL string
	}{
		{
			name:     "reading_progress",
			seedSQL:  `INSERT INTO reading_progress (article_id, scroll_position, user_id) VALUES ($1, 0.5, $2)`,
			countSQL: `SELECT COUNT(*) FROM reading_progress`,
		},
		{
			name:     "playback_progress",
			seedSQL:  `INSERT INTO playback_progress (article_id, position_seconds, user_id) VALUES ($1, 30, $2)`,
			countSQL: `SELECT COUNT(*) FROM playback_progress`,
		},
		{
			name:     "hidden_articles",
			seedSQL:  `INSERT INTO hidden_articles (article_id, user_id) VALUES ($1, $2)`,
			countSQL: `SELECT COUNT(*) FROM hidden_articles`,
		},
		{
			name:     "article_events",
			seedSQL:  `INSERT INTO article_events (article_id, user_id, event_type, occurred_at) VALUES ($1, $2, 'read', NOW())`,
			countSQL: `SELECT COUNT(*) FROM article_events`,
		},
		{
			name:     "user_tags",
			seedSQL:  `INSERT INTO user_tags (user_id, name) VALUES ($2, 'tag-' || $1::text)`,
			countSQL: `SELECT COUNT(*) FROM user_tags`,
		},
		{
			name:     "interest_topics",
			seedSQL:  `INSERT INTO interest_topics (user_id, topic, weight) VALUES ($2, 'topic-' || $1::text, 1.0)`,
			countSQL: `SELECT COUNT(*) FROM interest_topics`,
		},
		{
			name:     "interest_tags",
			seedSQL:  `INSERT INTO interest_tags (user_id, tag, weight) VALUES ($2, 'tag-' || $1::text, 1.0)`,
			countSQL: `SELECT COUNT(*) FROM interest_tags`,
		},
		{
			name:     "interest_categories",
			seedSQL:  `INSERT INTO interest_categories (user_id, category, weight) VALUES ($2, 'cat-' || $1::text, 1.0)`,
			countSQL: `SELECT COUNT(*) FROM interest_categories`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := newRLSFixture(t)
			defer cleanup()

			seedUserScopedRow(t, f.privDB, tc.seedSQL, f.articleA, f.userA)
			seedUserScopedRow(t, f.privDB, tc.seedSQL, f.articleB, f.userB)

			// Sanity: priv connection must see both rows (RLS bypassed).
			var total int
			if err := f.privDB.QueryRow(tc.countSQL).Scan(&total); err != nil {
				t.Fatalf("priv count: %v", err)
			}
			if total != 2 {
				t.Fatalf("priv seed sanity: want 2 rows, got %d", total)
			}

			f.asUser(t, f.userA, func(tx *sql.Tx) {
				var n int
				if err := tx.QueryRow(tc.countSQL).Scan(&n); err != nil {
					t.Fatalf("scoped count: %v", err)
				}
				if n != 1 {
					t.Errorf("userA scoped: want 1, got %d", n)
				}
			})
		})
	}
}

// ----------------------------------------------------------------------------
// WITH CHECK: userA cannot forge a row carrying userB's id.
// ----------------------------------------------------------------------------

func TestRLS_InsertWithWrongUserIDIsBlocked(t *testing.T) {
	f, cleanup := newRLSFixture(t)
	defer cleanup()

	f.asUser(t, f.userA, func(tx *sql.Tx) {
		if _, err := tx.Exec(`INSERT INTO user_tags (user_id, name) VALUES ($1, 'forged')`, f.userB); err == nil {
			t.Fatalf("userA forging row for userB should fail RLS WITH CHECK, but insert succeeded")
		}
	})
}

// ----------------------------------------------------------------------------
// Fail-closed: with no app.user_id set, private tables return zero rows.
// ----------------------------------------------------------------------------

func TestRLS_NoUserIDSeesNothingPrivate(t *testing.T) {
	f, cleanup := newRLSFixture(t)
	defer cleanup()
	seedUserScopedRow(t, f.privDB,
		`INSERT INTO reading_progress (article_id, scroll_position, user_id) VALUES ($1, 0.5, $2)`,
		f.articleA, f.userA)

	tx, err := f.appDB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM reading_progress`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("unset app.user_id leaks reading_progress (got %d rows)", n)
	}
}
