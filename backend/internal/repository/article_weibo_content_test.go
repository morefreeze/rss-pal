package repository_test

import (
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestUpdateEnrichedContentIfChanged(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var feedID, otherFeedID int
	if err := db.QueryRow(`
		INSERT INTO feeds (url, title)
		VALUES ('https://example.com/feed.xml', 'Test Feed')
		RETURNING id
	`).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO feeds (url, title)
		VALUES ('https://example.com/other.xml', 'Other Feed')
		RETURNING id
	`).Scan(&otherFeedID); err != nil {
		t.Fatalf("insert other feed: %v", err)
	}

	const articleURL = "https://weibo.com/1234567890/test"
	if _, err := db.Exec(`
		INSERT INTO articles (
			feed_id, title, url, content, published_at,
			summary_brief, summary_detailed,
			word_count, reading_minutes, refetch_attempts
		)
		VALUES ($1, 'Noisy post', $2, 'old noisy content', NOW(),
			'old brief summary', 'old detailed summary', 3, 1, 4)
	`, feedID, articleURL); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	repo := repository.NewArticleRepository(db)
	const enrichedContent = "原微博正文\n\n> 博主首评：补充信息"
	changed, err := repo.UpdateEnrichedContentIfChanged(feedID, articleURL, enrichedContent, 12, 2)
	if err != nil {
		t.Fatalf("UpdateEnrichedContentIfChanged: %v", err)
	}
	if !changed {
		t.Fatal("UpdateEnrichedContentIfChanged changed = false; want true")
	}

	var (
		content                       string
		wordCount, readingMinutes     int
		summaryBrief, summaryDetailed sql.NullString
		refetchAttempts               int
	)
	if err := db.QueryRow(`
		SELECT content, word_count, reading_minutes,
		       summary_brief, summary_detailed, refetch_attempts
		FROM articles
		WHERE feed_id = $1 AND url = $2
	`, feedID, articleURL).Scan(
		&content, &wordCount, &readingMinutes,
		&summaryBrief, &summaryDetailed, &refetchAttempts,
	); err != nil {
		t.Fatalf("query updated article: %v", err)
	}
	if content != enrichedContent {
		t.Fatalf("content = %q; want %q", content, enrichedContent)
	}
	if wordCount != 12 {
		t.Fatalf("word_count = %d; want 12", wordCount)
	}
	if readingMinutes != 2 {
		t.Fatalf("reading_minutes = %d; want 2", readingMinutes)
	}
	if summaryBrief.Valid {
		t.Fatalf("summary_brief = %q; want NULL", summaryBrief.String)
	}
	if summaryDetailed.Valid {
		t.Fatalf("summary_detailed = %q; want NULL", summaryDetailed.String)
	}
	if refetchAttempts != 0 {
		t.Fatalf("refetch_attempts = %d; want 0", refetchAttempts)
	}

	changed, err = repo.UpdateEnrichedContentIfChanged(feedID, articleURL, enrichedContent, 12, 2)
	if err != nil {
		t.Fatalf("second UpdateEnrichedContentIfChanged: %v", err)
	}
	if changed {
		t.Fatal("second UpdateEnrichedContentIfChanged changed = true; want false")
	}

	changed, err = repo.UpdateEnrichedContentIfChanged(otherFeedID, articleURL, "wrong feed content", 1, 1)
	if err != nil {
		t.Fatalf("wrong-feed UpdateEnrichedContentIfChanged: %v", err)
	}
	if changed {
		t.Fatal("wrong-feed UpdateEnrichedContentIfChanged changed = true; want false")
	}

	changed, err = repo.UpdateEnrichedContentIfChanged(feedID, "https://weibo.com/missing", "missing", 1, 1)
	if err != nil {
		t.Fatalf("missing-URL UpdateEnrichedContentIfChanged: %v", err)
	}
	if changed {
		t.Fatal("missing-URL UpdateEnrichedContentIfChanged changed = true; want false")
	}
}
