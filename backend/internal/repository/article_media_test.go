package repository_test

import (
	"database/sql"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestUpdateMediaRefreshesTranscriptEligibilityAndCanClearStaleMedia(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var feedID int
	if err := db.QueryRow(`
		INSERT INTO feeds (url, title)
		VALUES ('https://example.com/audio-feed', 'Audio feed')
		RETURNING id
	`).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	var articleID int
	if err := db.QueryRow(`
		INSERT INTO articles (
			feed_id, title, url, content, media_url, media_type,
			media_duration_seconds, transcript_fetched_at
		) VALUES ($1, 'Episode', 'https://example.com/episode', '正文',
			'https://cdn.example.com/old-episode.mp3', 'audio/mpeg', 30, NOW())
		RETURNING id
	`, feedID).Scan(&articleID); err != nil {
		t.Fatalf("insert article: %v", err)
	}

	repo := repository.NewArticleRepository(db)
	if err := repo.UpdateMedia(articleID, "https://cdn.example.com/new-episode.mp3", "audio/mpeg", 60); err != nil {
		t.Fatalf("UpdateMedia replace: %v", err)
	}
	assertStoredMedia(t, db, articleID, "https://cdn.example.com/new-episode.mp3", "audio/mpeg", 60, false)

	if _, err := db.Exec(`UPDATE articles SET transcript_fetched_at = NOW() WHERE id = $1`, articleID); err != nil {
		t.Fatalf("mark transcript attempted: %v", err)
	}
	if err := repo.UpdateMedia(articleID, "", "", 0); err != nil {
		t.Fatalf("UpdateMedia clear: %v", err)
	}
	assertStoredMedia(t, db, articleID, "", "", 0, false)
}

func assertStoredMedia(t *testing.T, db *sql.DB, articleID int, wantURL, wantType string, wantDuration int, wantTranscriptFetched bool) {
	t.Helper()
	var mediaURL, mediaType sql.NullString
	var duration sql.NullInt64
	var transcriptFetchedAt sql.NullTime
	if err := db.QueryRow(`
		SELECT media_url, media_type, media_duration_seconds, transcript_fetched_at
		FROM articles WHERE id = $1
	`, articleID).Scan(&mediaURL, &mediaType, &duration, &transcriptFetchedAt); err != nil {
		t.Fatalf("query stored media: %v", err)
	}
	if mediaURL.String != wantURL || mediaType.String != wantType || int(duration.Int64) != wantDuration {
		t.Fatalf("stored media = (%q, %q, %d), want (%q, %q, %d)", mediaURL.String, mediaType.String, duration.Int64, wantURL, wantType, wantDuration)
	}
	if transcriptFetchedAt.Valid != wantTranscriptFetched {
		t.Fatalf("transcript_fetched_at valid = %v, want %v", transcriptFetchedAt.Valid, wantTranscriptFetched)
	}
}
