package repository

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestServiceHeartbeatRepository_WithCtxBindsToTx(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	repo := NewServiceHeartbeatRepository(db)
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	bound := repo.WithCtx(fakeCtx{ctxkey.Tx: Querier(tx)})
	if bound == repo {
		t.Fatal("WithCtx should return a new instance when tx is present")
	}
	if bound.db != Querier(tx) {
		t.Fatal("WithCtx returned a repository whose db is not the supplied tx")
	}
}

func TestServiceHeartbeatRepository_WithCtxWithoutTxReturnsReceiver(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	repo := NewServiceHeartbeatRepository(db)
	if repo.WithCtx(fakeCtx{}) != repo {
		t.Fatal("WithCtx with no tx should return receiver")
	}
}
