package repository

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"sync"
	"testing"
)

func TestVerifiedIdentityAtomicAndNeverMergesByEmail(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	r := NewAccountAuthRepository(db)
	old, err := NewUserRepository(db).CreateAdmin("old", "old-password")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid, err := r.ResolveIdentity(ctx, "google", "subject-one", "same@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if uid == old.ID {
		t.Fatal("identity merged into existing account")
	}
	other, err := r.ResolveIdentity(ctx, "apple", "subject-two", "same@example.com", 0, true)
	if err != nil || other == uid {
		t.Fatalf("cross-provider merge: %d %v", other, err)
	}
	if _, err = r.ResolveIdentity(ctx, "google", "subject-one", "", old.ID, true); err == nil {
		t.Fatal("identity takeover allowed")
	}
	var admin bool
	var hash string
	if err = db.QueryRow(`SELECT is_admin,password_hash FROM users WHERE id=$1`, uid).Scan(&admin, &hash); err != nil || admin || hash != "!external" {
		t.Fatalf("unsafe new user: %v %q %v", admin, hash, err)
	}
}
func TestEmailChallengeSingleUseAndAttemptLimit(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	r := NewAccountAuthRepository(db)
	ctx := context.Background()
	if err := r.CreateEmailChallenge(ctx, "challenge", "a@example.com", "hash", "ip", 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	successes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, e := r.VerifyEmailChallenge(ctx, "challenge", "hash", 0, true)
			if e == nil {
				successes <- id
			}
		}()
	}
	wg.Wait()
	close(successes)
	if len(successes) != 1 {
		t.Fatalf("concurrent consume successes %d", len(successes))
	}
	if err := r.CreateEmailChallenge(ctx, "attempts", "b@example.com", "right", "other-ip", 0); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := r.VerifyEmailChallenge(ctx, "attempts", "wrong", 0, true); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if _, err := r.VerifyEmailChallenge(ctx, "attempts", "right", 0, true); err == nil {
		t.Fatal("exhausted challenge accepted")
	}
}

func TestAccountAuthRuntimeRoleAndSendingLimits(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SET ROLE rsspal_app`); err != nil {
		t.Fatal(err)
	}
	r := NewAccountAuthRepository(db)
	ctx := context.Background()
	uid, err := r.ResolveIdentity(ctx, "email", "runtime@example.com", "runtime@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	var subscriptions int
	if err = db.QueryRow(`SELECT count(*) FROM feeds WHERE owner_id=$1`, uid).Scan(&subscriptions); err != nil || subscriptions != 0 {
		t.Fatalf("new user subscriptions %d %v", subscriptions, err)
	}
	if err = r.CreateEmailChallenge(ctx, "first", "budget@example.com", "hash", "network", 0); err != nil {
		t.Fatal(err)
	}
	if err = r.CreateEmailChallenge(ctx, "second", "budget@example.com", "hash", "network", 0); err != ErrAccountRateLimit {
		t.Fatalf("resend limit %v", err)
	}
	if _, err = db.Exec(`UPDATE account_email_challenges SET expires_at=now()-interval '1 second',created_at=now()-interval '2 minutes' WHERE id='first'`); err != nil {
		t.Fatal(err)
	}
	if _, err = r.VerifyEmailChallenge(ctx, "first", "hash", 0, true); err == nil {
		t.Fatal("expired challenge accepted")
	}
	// Seed historical sends so the same production SQL enforces IP and global caps.
	if _, err = db.Exec(`INSERT INTO account_email_challenges(id,email,code_hash,ip_hash,created_at) SELECT 'ip-'||i,'ip-'||i||'@example.com','hash','limited-ip',now()-interval '2 minutes' FROM generate_series(1,20) i`); err != nil {
		t.Fatal(err)
	}
	if err = r.CreateEmailChallenge(ctx, "blocked-ip", "new@example.com", "hash", "limited-ip", 0); err != ErrAccountRateLimit {
		t.Fatalf("IP limit %v", err)
	}
	if _, err = db.Exec(`INSERT INTO account_email_challenges(id,email,code_hash,ip_hash,created_at) SELECT 'global-'||i,'global-'||i||'@example.com','hash','global-'||i,now()-interval '2 minutes' FROM generate_series(1,200) i`); err != nil {
		t.Fatal(err)
	}
	if err = r.CreateEmailChallenge(ctx, "blocked-global", "new@example.com", "hash", "new-ip", 0); err != ErrAccountRateLimit {
		t.Fatalf("global limit %v", err)
	}
}
