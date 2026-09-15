package repository_test

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestRegisterInviteConsumedOnceConcurrently(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewUserRepository(db)
	admin, err := repo.CreateAdmin("invite-admin", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	invite, err := repo.CreateInviteCode(admin.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if _, err := repo.Register(fmt.Sprintf("racer-%d", i), "test-password", invite.Code); err == nil {
				successes.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Fatalf("one invite created %d users; want exactly one", got)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE username LIKE 'racer-%'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed users = %d", count)
	}
}

func TestRegisterFailureDoesNotConsumeInvite(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := repository.NewUserRepository(db)
	admin, err := repo.CreateAdmin("existing", "test-password")
	if err != nil {
		t.Fatal(err)
	}
	invite, err := repo.CreateInviteCode(admin.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Register("existing", "test-password", invite.Code); err == nil {
		t.Fatal("duplicate username accepted")
	}
	if _, err := repo.Register("fresh", "test-password", invite.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Register("another", "test-password", invite.Code); err == nil {
		t.Fatal("used invite accepted")
	}
	expired, err := repo.CreateInviteCode(admin.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE invite_codes SET expires_at = NOW() - interval '1 minute' WHERE id = $1`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Register("expired", "test-password", expired.Code); err == nil {
		t.Fatal("expired invite accepted")
	}
}
