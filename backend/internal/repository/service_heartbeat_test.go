package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestServiceHeartbeatRepository_BeatAndLastSeen(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	repo := repository.NewServiceHeartbeatRepository(db)
	if err := repo.Beat("worker"); err != nil {
		t.Fatalf("Beat: %v", err)
	}

	first, err := repo.LastSeen("worker")
	if err != nil {
		t.Fatalf("LastSeen after first Beat: %v", err)
	}
	if age := time.Since(first); age < 0 || age > 5*time.Second {
		t.Fatalf("first heartbeat age=%s, want between 0 and 5s", age)
	}

	time.Sleep(25 * time.Millisecond)
	if err := repo.Beat("worker"); err != nil {
		t.Fatalf("second Beat: %v", err)
	}

	second, err := repo.LastSeen("worker")
	if err != nil {
		t.Fatalf("LastSeen after second Beat: %v", err)
	}
	if !second.After(first) {
		t.Fatalf("second heartbeat=%s, want after first=%s", second, first)
	}
}

func TestServiceHeartbeatRepository_LastSeenMissing(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	_, err := repository.NewServiceHeartbeatRepository(db).LastSeen("missing")
	if !errors.Is(err, repository.ErrHeartbeatNotFound) {
		t.Fatalf("LastSeen missing error=%v, want %v", err, repository.ErrHeartbeatNotFound)
	}
}
