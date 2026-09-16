package repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestManualInterestReservationAtomicAndFailedAttemptsCount(t *testing.T) {
	db, schema, cleanup := testdb.NewWithSchema(t)
	defer cleanup()
	app, closeApp := testdb.NewAsApp(t, schema)
	defer closeApp()
	var uid int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('reserve-owner','x') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	repo := NewUserInterestRepository(app)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, err := repo.ReserveManual(context.Background(), uid, "test", 3, 100)
			if err == nil {
				accepted.Add(1)
				if e := repo.MarkFailedForUser(id, uid, "test failure"); e != nil {
					t.Error(e)
				}
			} else if !errors.Is(err, ErrInterestQuota) && !errors.Is(err, ErrPendingExists) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() > 3 || accepted.Load() == 0 {
		t.Fatalf("accepted=%d", accepted.Load())
	}
	for accepted.Load() < 3 {
		id, err := repo.ReserveManual(context.Background(), uid, "test", 3, 100)
		if err != nil {
			t.Fatal(err)
		}
		if err = repo.MarkFailedForUser(id, uid, "failed"); err != nil {
			t.Fatal(err)
		}
		accepted.Add(1)
	}
	if _, err := repo.ReserveManual(context.Background(), uid, "test", 3, 100); !errors.Is(err, ErrInterestQuota) {
		t.Fatalf("failed attempts bypassed quota: %v", err)
	}
}
