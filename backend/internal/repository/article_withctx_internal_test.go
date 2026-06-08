package repository

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/ctxkey"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

type fakeCtx map[string]any

func (f fakeCtx) Get(k string) (any, bool) {
	v, ok := f[k]
	return v, ok
}

func TestArticleRepository_WithCtx_BindsToTx(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	repo := NewArticleRepository(db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	bound := repo.WithCtx(fakeCtx{ctxkey.Tx: Querier(tx)})

	if bound == repo {
		t.Fatalf("WithCtx should return a new instance when tx is present")
	}
	if bound.querier() != Querier(tx) {
		t.Fatalf("WithCtx returned a repo whose db is not the supplied tx")
	}
}

func TestArticleRepository_WithCtx_NoTxReturnsReceiver(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	repo := NewArticleRepository(db)
	if repo.WithCtx(fakeCtx{}) != repo {
		t.Fatalf("WithCtx with empty context should return receiver")
	}
}

func TestArticleRepository_WithCtx_NonQuerierValueIgnored(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	repo := NewArticleRepository(db)
	// Put a non-Querier value under "tx" — WithCtx should fall through.
	if repo.WithCtx(fakeCtx{ctxkey.Tx: "not a querier"}) != repo {
		t.Fatalf("WithCtx with wrong-type value should return receiver")
	}
}
