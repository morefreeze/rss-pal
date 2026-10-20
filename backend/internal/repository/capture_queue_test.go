package repository

import (
	"fmt"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
)

func TestBrowserCaptureCompletesQueueBeforeStaleWorker(t *testing.T) {
	for _, failures := range []int{0, 3} {
		t.Run(fmt.Sprint(failures), func(t *testing.T) {
			db, cleanup := testdb.New(t)
			defer cleanup()
			var owner int
			if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('browser-capture','x') RETURNING id`).Scan(&owner); err != nil {
				t.Fatal(err)
			}
			r := NewArticleRepository(db)
			a, err := r.EnqueueCapture(owner, "https://example.test/browser")
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < failures; i++ {
				if err := r.FailQueuedCaptureAttempt(a.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.UpdateContent(a.ID, "Browser's already loaded body", 8, 1); err != nil {
				t.Fatal(err)
			}
			if err := r.CompleteQueuedCapture(a.ID, "Stale server body", "Stale title", nil, 1, 1); err != nil {
				t.Fatal(err)
			}
			if err := r.FailQueuedCaptureAttempt(a.ID); err != nil {
				t.Fatal(err)
			}
			var state, body string
			var attempts int
			if err := db.QueryRow(`SELECT processing_state,content,refetch_attempts FROM articles WHERE id=$1`, a.ID).Scan(&state, &body, &attempts); err != nil {
				t.Fatal(err)
			}
			if state != "ready" || body != "Browser's already loaded body" || attempts != 0 {
				t.Fatalf("state=%s body=%q attempts=%d", state, body, attempts)
			}
		})
	}
}
