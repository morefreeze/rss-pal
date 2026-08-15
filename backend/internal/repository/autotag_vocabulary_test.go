package repository_test

import (
	"reflect"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestGetTagNamesForUser(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var userA, userB int
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('a', 'x') RETURNING id`).Scan(&userA); err != nil {
		t.Fatalf("insert user A: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO users (username, password_hash) VALUES ('b', 'x') RETURNING id`).Scan(&userB); err != nil {
		t.Fatalf("insert user B: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_tags (user_id, name)
		VALUES ($1, 'OpenAI'), ($1, 'RAG'), ($2, 'Private')
	`, userA, userB); err != nil {
		t.Fatalf("insert tags: %v", err)
	}

	got, err := repository.NewUserTagRepository(db).GetTagNamesForUser(userA)
	if err != nil {
		t.Fatalf("GetTagNamesForUser: %v", err)
	}
	want := []string{"OpenAI", "RAG"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names = %#v, want %#v", got, want)
	}
}

func TestGetTopTagVocabulary(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	var feedID int
	if err := db.QueryRow(`INSERT INTO feeds (url, title) VALUES ('https://example.com/feed', 'Example') RETURNING id`).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	for _, row := range []struct {
		title string
		tags  string
	}{
		{"a", `ARRAY['OpenAI','RAG']`},
		{"b", `ARRAY['OpenAI','向量数据库']`},
		{"c", `ARRAY['OpenAI','RAG']`},
	} {
		if _, err := db.Exec(`
			INSERT INTO articles (feed_id, title, url, content, published_at, tags)
			VALUES ($1, $2, 'https://example.com/' || $2, '正文', NOW(), `+row.tags+`)
		`, feedID, row.title); err != nil {
			t.Fatalf("insert article %s: %v", row.title, err)
		}
	}

	got, err := repository.NewArticleRepository(db).GetTopTagVocabulary(2)
	if err != nil {
		t.Fatalf("GetTopTagVocabulary: %v", err)
	}
	want := []string{"OpenAI", "RAG"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("vocabulary = %#v, want %#v", got, want)
	}
}
