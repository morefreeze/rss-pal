package englife

import (
	"context"
	"database/sql"
	"errors"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
	"time"
)

func TestSQLStoreLocksPairingAndPersistentJobs(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	ctx := context.Background()
	store := NewSQLStore(db)
	var id int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash,is_admin) VALUES('englife-admin','unused',true) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	unlock, err := store.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewSQLStore(db).Lock(ctx); !errors.Is(err, ErrPending) {
		t.Fatal("parallel lock accepted", err)
	}
	unlock()
	if err = store.CreatePair(ctx, "hash", id, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PairAdmin(ctx, "hash"); err != nil {
		t.Fatal(err)
	}
	c := Connection{Secret: []byte("encrypted"), AccountID: "a", Account: "account", State: "ready", CheckedAt: time.Now()}
	if err = store.CompletePair(ctx, "hash", c); err != nil {
		t.Fatal(err)
	}
	if err = store.CompletePair(ctx, "hash", c); !errors.Is(err, ErrPair) {
		t.Fatal("replay", err)
	}
	j := Job{AccountID: "a", VideoID: "abcdefghijk", RemoteID: "job", Status: "pending", NextCheck: time.Now(), UpdatedAt: time.Now()}
	if err = store.SaveJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	got, err := NewSQLStore(db).Job(ctx, "a", "abcdefghijk")
	if err != nil || got.RemoteID != "job" {
		t.Fatalf("lost job %+v %v", got, err)
	}
	if _, err = store.Job(ctx, "b", "abcdefghijk"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross account job leak", err)
	}
	if err = store.CreatePair(ctx, "hash2", id, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE users SET is_admin=false WHERE id=$1`, id)
	if _, err = store.PairAdmin(ctx, "hash2"); !errors.Is(err, ErrPair) {
		t.Fatal("demoted admin paired", err)
	}
	if err = store.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	gotC, err := store.Connection(ctx)
	if err != nil || len(gotC.Secret) != 0 {
		t.Fatal("secret retained")
	}
}
