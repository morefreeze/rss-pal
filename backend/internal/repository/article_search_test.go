package repository_test

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestArticleSearchMatchesFeedMetadata(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var ruanFeedID int
	if err := db.QueryRow(`
		INSERT INTO feeds (url, title)
		VALUES ('https://www.ruanyifeng.com/blog/atom.xml', '阮一峰的网络日志')
		RETURNING id
	`).Scan(&ruanFeedID); err != nil {
		t.Fatalf("insert ruan feed: %v", err)
	}
	var otherFeedID int
	if err := db.QueryRow(`
		INSERT INTO feeds (url, title)
		VALUES ('https://example.com/feed.xml', '普通博客')
		RETURNING id
	`).Scan(&otherFeedID); err != nil {
		t.Fatalf("insert other feed: %v", err)
	}

	var weeklyID int
	if err := db.QueryRow(`
		INSERT INTO articles (feed_id, title, url, content, published_at, summary_brief)
		VALUES ($1, '科技爱好者周刊（第 301 期）', 'https://weekly.example.com/301', '正文', NOW(), '摘要')
		RETURNING id
	`, ruanFeedID).Scan(&weeklyID); err != nil {
		t.Fatalf("insert weekly article: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO articles (feed_id, title, url, content, published_at, summary_brief)
		VALUES ($1, '无关文章', 'https://example.com/1', '正文', NOW(), '摘要')
	`, otherFeedID); err != nil {
		t.Fatalf("insert other article: %v", err)
	}
	articles, err := repository.NewArticleRepository(db).Search("atom", 1, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("Search(atom) returned %d articles, want 1: %+v", len(articles), articles)
	}
	if articles[0].ID != weeklyID {
		t.Fatalf("Search(atom) returned article %d, want %d", articles[0].ID, weeklyID)
	}
	if articles[0].FeedTitle != "阮一峰的网络日志" {
		t.Fatalf("FeedTitle = %q, want 阮一峰的网络日志", articles[0].FeedTitle)
	}
}
