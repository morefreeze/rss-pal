package repository

import (
	"sync"
	"testing"
)

func TestCaptureLinkTitleMatchesReaderURLIdentity(t *testing.T) {
	for _, tc := range []struct {
		body, target, title string
		matches             bool
	}{
		{"[Label](https://example.com/a?utm_source=x#section)", "https://example.com/a", "Label", true},
		{"[Self anchor](https://example.com/page/#how-i-built-this)", "https://example.com/page", "Self anchor", true},
		{"<a href=\"https://example.com/a?x=1&amp;y=2\">HTML</a>", "https://example.com/a?x=1&y=2", "https://example.com/a?x=1&y=2", true},
		{"[Wikipedia](https://example.com/a_(b))", "https://example.com/a_(b)", "Wikipedia", true},
		{"[Long](https://example.com/abc)", "https://example.com/a", "", false},
		{"[Other host](https://example.com.attacker.test/a)", "https://example.com/a", "", false},
	} {
		title, ok := captureLinkTitle(tc.body, tc.target)
		if ok != tc.matches || title != tc.title {
			t.Errorf("body=%q target=%q got(%q,%v) want(%q,%v)", tc.body, tc.target, title, ok, tc.title, tc.matches)
		}
	}
}

func TestShortcutDraftConcurrentSharesAndConfirmation(t *testing.T) {
	repo, db, feed, parent := seedLinkSetParent(t)
	var owner int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('concurrent-drafts','x') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE feeds SET owner_id=$1 WHERE id=$2`, owner, feed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE articles SET content='[Link](https://example.com/pending#section)' WHERE id=$1`, parent); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	failures := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := repo.AddShortcutCaptureDraft(owner, 0, "https://example.com/pending#section")
			failures <- err
		}()
	}
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var drafts int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1`, parent).Scan(&drafts); err != nil || drafts != 1 {
		t.Fatalf("dedup drafts=%d err=%v", drafts, err)
	}
	start = make(chan struct{})
	failures = make(chan error, 16)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := repo.AddShortcutCaptureDraft(owner, 0, "https://example.com/pending")
			failures <- err
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, err := repo.EnableAndInsertLinkSetChildren(parent, []LinkSetChildInput{{FeedID: feed, ParentArticleID: parent, URL: "https://example.com/pending", Title: "Link", ProcessingState: "processing"}})
		failures <- err
	}()
	close(start)
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var children int
	if err := db.QueryRow(`SELECT count(*) FROM article_capture_drafts WHERE article_id=$1`, parent).Scan(&drafts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM articles WHERE parent_article_id=$1`, parent).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if drafts != 0 || children != 1 {
		t.Fatalf("concurrent share resurrected draft: drafts=%d children=%d", drafts, children)
	}
}
