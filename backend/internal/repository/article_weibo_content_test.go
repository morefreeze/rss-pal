package repository_test

import (
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

type articleRefreshState struct {
	content                       string
	wordCount, readingMinutes     int
	summaryBrief, summaryDetailed sql.NullString
	refetchAttempts               int
	parentArticleID               sql.NullInt64
	isClip                        bool
}

func readArticleRefreshState(t *testing.T, db *sql.DB, articleID int) articleRefreshState {
	t.Helper()

	var state articleRefreshState
	if err := db.QueryRow(`
		SELECT content, word_count, reading_minutes,
		       summary_brief, summary_detailed, refetch_attempts,
		       parent_article_id, is_clip
		FROM articles
		WHERE id = $1
	`, articleID).Scan(
		&state.content, &state.wordCount, &state.readingMinutes,
		&state.summaryBrief, &state.summaryDetailed, &state.refetchAttempts,
		&state.parentArticleID, &state.isClip,
	); err != nil {
		t.Fatalf("query article %d: %v", articleID, err)
	}
	return state
}

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
	var articleID int
	if err := db.QueryRow(`
		INSERT INTO articles (
			feed_id, title, url, content, published_at,
			summary_brief, summary_detailed,
			word_count, reading_minutes, refetch_attempts,
			parent_article_id, is_clip
		)
		VALUES ($1, 'Noisy post', $2, 'old noisy content', NOW(),
			'old brief summary', 'old detailed summary', 3, 1, 4,
			NULL, false)
		RETURNING id
	`, feedID, articleURL).Scan(&articleID); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	var childArticleID int
	if err := db.QueryRow(`
		INSERT INTO articles (
			feed_id, title, url, content, published_at,
			summary_brief, summary_detailed,
			word_count, reading_minutes, refetch_attempts,
			parent_article_id
		)
		VALUES ($1, 'Child duplicate', $2, 'child original content', NOW(),
			'child brief summary', 'child detailed summary', 5, 2, 7, $3)
		RETURNING id
	`, feedID, articleURL, articleID).Scan(&childArticleID); err != nil {
		t.Fatalf("insert child article: %v", err)
	}

	var clipArticleID int
	if err := db.QueryRow(`
		INSERT INTO articles (
			feed_id, title, url, content, published_at,
			summary_brief, summary_detailed,
			word_count, reading_minutes, refetch_attempts,
			is_clip
		)
		VALUES ($1, 'Clip duplicate', $2, 'clip original content', NOW(),
			'clip brief summary', 'clip detailed summary', 6, 3, 8, true)
		RETURNING id
	`, feedID, articleURL).Scan(&clipArticleID); err != nil {
		t.Fatalf("insert clip article: %v", err)
	}

	childBefore := readArticleRefreshState(t, db, childArticleID)
	clipBefore := readArticleRefreshState(t, db, clipArticleID)

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
		WHERE id = $1
	`, articleID).Scan(
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
	if childAfter := readArticleRefreshState(t, db, childArticleID); childAfter != childBefore {
		t.Fatalf("child article changed:\n got: %#v\nwant: %#v", childAfter, childBefore)
	}
	if clipAfter := readArticleRefreshState(t, db, clipArticleID); clipAfter != clipBefore {
		t.Fatalf("clip article changed:\n got: %#v\nwant: %#v", clipAfter, clipBefore)
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

func TestUpdateSummaryIfContentUnchangedRejectsStaleWriter(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var feedID int
	if err := db.QueryRow(`
		INSERT INTO feeds (url, title)
		VALUES ('https://example.com/cas-feed.xml', 'CAS Feed')
		RETURNING id
	`).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}

	const (
		articleURL = "https://weibo.com/1234567890/cas-test"
		oldContent = "old content used by an in-flight summary"
		newContent = "new enriched content\n\n### 博主首评\n\n补充信息"
	)
	var articleID int
	if err := db.QueryRow(`
		INSERT INTO articles (
			feed_id, title, url, content, published_at,
			summary_brief, summary_detailed, processing_state
		)
		VALUES ($1, 'CAS article', $2, $3, NOW(),
			'old brief', 'old detailed', 'processing')
		RETURNING id
	`, feedID, articleURL, oldContent).Scan(&articleID); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	repo := repository.NewArticleRepository(db)
	refreshed, err := repo.UpdateEnrichedContentIfChanged(feedID, articleURL, newContent, 10, 1)
	if err != nil {
		t.Fatalf("UpdateEnrichedContentIfChanged: %v", err)
	}
	if !refreshed {
		t.Fatal("UpdateEnrichedContentIfChanged changed = false; want true")
	}

	updated, err := repo.UpdateSummaryIfContentUnchanged(articleID, oldContent, "stale brief", "stale detailed")
	if err != nil {
		t.Fatalf("stale UpdateSummaryIfContentUnchanged: %v", err)
	}
	if updated {
		t.Fatal("stale UpdateSummaryIfContentUnchanged updated = true; want false")
	}

	var brief, detailed sql.NullString
	var processingState string
	if err := db.QueryRow(`
		SELECT summary_brief, summary_detailed, processing_state
		FROM articles
		WHERE id = $1
	`, articleID).Scan(&brief, &detailed, &processingState); err != nil {
		t.Fatalf("query after stale summary: %v", err)
	}
	if brief.Valid || detailed.Valid {
		t.Fatalf("stale summary persisted: brief=%q detailed=%q", brief.String, detailed.String)
	}
	if processingState != "processing" {
		t.Fatalf("processing_state after stale summary = %q; want processing", processingState)
	}

	updated, err = repo.UpdateSummaryIfContentUnchanged(articleID, newContent, "fresh brief", "fresh detailed")
	if err != nil {
		t.Fatalf("fresh UpdateSummaryIfContentUnchanged: %v", err)
	}
	if !updated {
		t.Fatal("fresh UpdateSummaryIfContentUnchanged updated = false; want true")
	}

	if err := db.QueryRow(`
		SELECT summary_brief, summary_detailed, processing_state
		FROM articles
		WHERE id = $1
	`, articleID).Scan(&brief, &detailed, &processingState); err != nil {
		t.Fatalf("query after fresh summary: %v", err)
	}
	if !brief.Valid || brief.String != "fresh brief" {
		t.Fatalf("summary_brief = %#v; want fresh brief", brief)
	}
	if !detailed.Valid || detailed.String != "fresh detailed" {
		t.Fatalf("summary_detailed = %#v; want fresh detailed", detailed)
	}
	if processingState != "ready" {
		t.Fatalf("processing_state after fresh summary = %q; want ready", processingState)
	}
}
