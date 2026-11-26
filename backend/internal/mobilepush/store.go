package mobilepush

import (
	"context"
	"database/sql"
	"errors"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/bytedance/rss-pal/internal/repository"
)

type Keys struct {
	Private string
	Public  string
}

func LoadKeys(ctx context.Context, db *sql.DB) (Keys, error) {
	const lookup = "SELECT private_key, public_key FROM mobile_push_keys WHERE singleton"
	var keys Keys
	err := db.QueryRowContext(ctx, lookup).Scan(&keys.Private, &keys.Public)
	if !errors.Is(err, sql.ErrNoRows) {
		return keys, err
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, err
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO mobile_push_keys (private_key, public_key)
		VALUES ($1, $2) ON CONFLICT (singleton) DO NOTHING
	`, private, public); err != nil {
		return Keys{}, err
	}
	err = db.QueryRowContext(ctx, lookup).Scan(&keys.Private, &keys.Public)
	return keys, err
}

func Enrolled(ctx context.Context, db repository.Querier, userID int, endpoint string) (bool, error) {
	var active bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM mobile_push_subscriptions WHERE user_id = $1 AND endpoint = $2)
	`, userID, endpoint).Scan(&active)
	return active, err
}

var ErrEndpointOwned = errors.New("push endpoint belongs to another account")

func Enroll(ctx context.Context, db repository.Querier, userID int, sub webpush.Subscription) error {
	result, err := db.ExecContext(ctx, `
		INSERT INTO mobile_push_subscriptions (endpoint, user_id, auth, p256dh)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (endpoint) DO NOTHING
	`, sub.Endpoint, userID, sub.Keys.Auth, sub.Keys.P256dh)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); n != 0 || err != nil {
		return err
	}
	result, err = db.ExecContext(ctx, `
		UPDATE mobile_push_subscriptions SET auth = $3, p256dh = $4
		WHERE endpoint = $1 AND user_id = $2
	`, sub.Endpoint, userID, sub.Keys.Auth, sub.Keys.P256dh)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n == 0 && err == nil {
		return ErrEndpointOwned
	}
	return err
}

func Remove(ctx context.Context, db repository.Querier, userID int, endpoint string) error {
	_, err := db.ExecContext(ctx, `
		DELETE FROM mobile_push_subscriptions WHERE user_id = $1 AND endpoint = $2
	`, userID, endpoint)
	return err
}
