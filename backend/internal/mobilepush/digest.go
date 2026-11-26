package mobilepush

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/bytedance/rss-pal/internal/repository"
)

type deliverDigest func(context.Context, webpush.Subscription, int, int) (int, error)

func Run(ctx context.Context, db *sql.DB) {
	var deliver deliverDigest
	send := func(ctx context.Context, sub webpush.Subscription, recent, total int) (int, error) {
		if deliver == nil {
			keys, err := LoadKeys(ctx, db)
			if err != nil {
				return 0, fmt.Errorf("load Web Push keys: %w", err)
			}
			deliver = newSender(keys).send
		}
		return deliver(ctx, sub, recent, total)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for ctx.Err() == nil {
		cycle, cancel := context.WithTimeout(ctx, 2*time.Minute)
		for cycle.Err() == nil {
			handled, err := processNext(cycle, db, send)
			if err != nil {
				log.Printf("Mobile push digest: %v", err)
				break
			}
			if !handled {
				break
			}
		}
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func processNext(ctx context.Context, db *sql.DB, send deliverDigest) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var sub webpush.Subscription
	var userID int
	var previous string
	err = tx.QueryRowContext(ctx, `
		SELECT endpoint, auth, p256dh, user_id, checked_snapshot::text
		FROM mobile_push_subscriptions
		WHERE due_at <= NOW()
		ORDER BY due_at LIMIT 1 FOR UPDATE SKIP LOCKED
	`).Scan(&sub.Endpoint, &sub.Keys.Auth, &sub.Keys.P256dh, &userID, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	total, recent, checkpoint, err := repository.UnreadDigest(ctx, tx, userID, previous)
	if err != nil {
		return false, err
	}
	status := http.StatusOK
	if recent > 0 {
		status, err = send(ctx, sub, recent, total)
		if err != nil {
			log.Printf("Mobile push delivery: %v", err)
		}
	}
	switch {
	case status >= 200 && status < 300 && err == nil:
		_, err = tx.ExecContext(ctx, `
			UPDATE mobile_push_subscriptions SET checked_snapshot = $2::pg_snapshot, due_at = NOW() + INTERVAL '3 hours'
			WHERE endpoint = $1
		`, sub.Endpoint, checkpoint)
	case err == nil && status < 500 && status != http.StatusTooManyRequests &&
		status != http.StatusRequestTimeout:
		_, err = tx.ExecContext(ctx, "DELETE FROM mobile_push_subscriptions WHERE endpoint = $1", sub.Endpoint)
	default:
		if status != 0 {
			log.Printf("Mobile push provider returned HTTP %d", status)
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE mobile_push_subscriptions SET due_at = NOW() + INTERVAL '15 minutes' WHERE endpoint = $1
		`, sub.Endpoint)
	}
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}
