package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"github.com/bytedance/rss-pal/internal/rss"
)

type htmlDateBodyFixture struct{}

func (htmlDateBodyFixture) FetchContent(context.Context, string) (string, error) {
	return "HTML article content from fixture", nil
}

func TestHTMLFeedPublicationDatesAtBothWriteBoundaries(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	const valid = "2026-09-20T08:30:00Z"
	cases := []struct {
		name, published, updated, want string
		exists                         bool
	}{
		{"new-zero", "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z", "", false},
		{"existing-zero", "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z", "", true},
		{"new-updated", "0001-01-01T00:00:00Z", valid, valid, false},
		{"existing-updated", "0001-01-01T00:00:00Z", valid, valid, true},
		{"valid-published", valid, "2026-09-21T08:30:00Z", valid, false},
	}
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/csdn/blog/datefixture" {
			w.Header().Set("Content-Type", "application/atom+xml")
			fmt.Fprint(w, `<feed xmlns="http://www.w3.org/2005/Atom"><title>HTML resolved date fixture</title><id>date-fixture</id>`)
			for _, tc := range cases {
				fmt.Fprintf(w, `<entry><title>%s</title><id>%s</id><link href="%s/post/%s"/><published>%s</published><updated>%s</updated></entry>`, tc.name, tc.name, serverURL, tc.name, tc.published, tc.updated)
			}
			fmt.Fprint(w, `</feed>`)
			return
		}
		t.Errorf("unexpected body request %s: date test supplies a body fixture", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	serverURL = server.URL
	feeds := repository.NewFeedRepository(db)
	articles := repository.NewArticleRepository(db)
	feed := model.Feed{URL: "https://blog.csdn.net/datefixture", FeedType: "html", FetchIntervalMin: 60}
	if err := feeds.Create(&feed); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		if tc.exists {
			article := &model.Article{FeedID: feed.ID, Title: tc.name, URL: server.URL + "/post/" + tc.name, Content: "Existing body"}
			if err := articles.Create(article); err != nil {
				t.Fatal(err)
			}
		}
	}
	processHTMLFeed(context.Background(), feeds, articles, rss.NewFetcher(server.URL), htmlDateBodyFixture{}, nil, feed)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got sql.NullTime
			if err := db.QueryRow(`SELECT published_at FROM articles WHERE feed_id=$1 AND url=$2`, feed.ID, server.URL+"/post/"+tc.name).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if got.Valid {
					t.Fatalf("unknown date persisted: %v", got.Time)
				}
			} else if !got.Valid || got.Time.UTC().Format(time.RFC3339) != tc.want {
				t.Fatalf("valid source date lost: got %v, want %s", got, tc.want)
			}
		})
	}
}
