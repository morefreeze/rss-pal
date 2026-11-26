package mobilepush

import (
	"context"
	"errors"
	"net/http"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestDigestWaitsForWindowAndUsesAccountUnreadCount(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO users(id,username,password_hash) VALUES (700,'push-owner','x'), (701,'push-other','x')`)
	exec(`INSERT INTO feeds(id,owner_id,url) VALUES
		(700,700,'https://example.org/own'), (701,701,'https://example.org/other')`)
	exec(`INSERT INTO articles(id,feed_id,title,url) VALUES (710,700,'existing','https://example.org/710')`)
	sub := webpush.Subscription{Endpoint: "https://web.push.apple.com/test-digest"}
	if err := Enroll(ctx, db, 700, sub); err != nil {
		t.Fatal(err)
	}
	if err := Enroll(ctx, db, 701, sub); !errors.Is(err, ErrEndpointOwned) {
		t.Fatalf("another account registered the device: %v", err)
	}
	exec(`INSERT INTO articles(id,feed_id,title,url) VALUES
		(711,700,'new','https://example.org/711'),
		(712,700,'read','https://example.org/712'),
		(713,700,'hidden','https://example.org/713'),
		(714,701,'other','https://example.org/714')`)
	exec(`INSERT INTO reading_progress(user_id,article_id,is_completed) VALUES (700,712,true)`)
	exec(`INSERT INTO hidden_articles(user_id,article_id) VALUES (700,713)`)
	late, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Rollback()
	if _, err := late.ExecContext(ctx, `INSERT INTO articles(id,feed_id,title,url) VALUES (715,700,'late','https://example.org/715')`); err != nil {
		t.Fatal(err)
	}

	deliveries := 0
	var send deliverDigest
	send = func(_ context.Context, actual webpush.Subscription, recent, total int) (int, error) {
		deliveries++
		want, err := repository.NewArticleRepository(db).GetUnreadCount(700)
		if err != nil {
			t.Fatal(err)
		}
		if actual.Endpoint != sub.Endpoint || recent != 1 || total != deliveries+1 || total != want {
			t.Fatalf("delivery has recent=%d total=%d, want 1 recent and %d total", recent, total, want)
		}
		if handled, err := processNext(ctx, db, send); err != nil || handled {
			t.Fatalf("locked device processed again: handled=%v err=%v", handled, err)
		}
		return http.StatusCreated, nil
	}
	check := func(want bool) {
		t.Helper()
		if handled, err := processNext(ctx, db, send); err != nil || handled != want {
			t.Fatalf("processed=%v err=%v want processed=%v", handled, err, want)
		}
	}

	check(false)
	exec(`UPDATE mobile_push_subscriptions SET due_at = NOW()`)
	check(true)
	check(false)
	if err := late.Commit(); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE mobile_push_subscriptions SET due_at = NOW()`)
	check(true)
	exec(`UPDATE mobile_push_subscriptions SET due_at = NOW()`)
	check(true)
	if deliveries != 2 {
		t.Fatalf("sent %d digests; the late commit should send and the empty window should stay quiet", deliveries)
	}
	if err := Remove(ctx, db, 701, sub.Endpoint); err != nil {
		t.Fatal(err)
	}
	if active, err := Enrolled(ctx, db, 700, sub.Endpoint); err != nil || !active {
		t.Fatalf("another account removed the device: active=%v err=%v", active, err)
	}
	if err := Remove(ctx, db, 700, sub.Endpoint); err != nil {
		t.Fatal(err)
	}
	if active, err := Enrolled(ctx, db, 700, sub.Endpoint); err != nil || active {
		t.Fatalf("device still active after opt-out: active=%v err=%v", active, err)
	}
}
