package repository

import (
	"context"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"testing"
)

func TestQQIdentityMigrationUpgradeAndReplay(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "068_read_undo.sql")
	defer cleanup()
	repo := NewAccountAuthRepository(db)
	ctx := context.Background()
	uid, err := repo.ResolveIdentity(ctx, "github", "old-gh-id", "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := testdb.ExecuteMigrationFile(db, "069_qq_account_identity.sql"); err != nil {
		t.Fatal(err)
	}
	bound, err := repo.ResolveIdentity(ctx, "qq", "123456:opaque-QQ-ID", "", uid, true)
	if err != nil || bound != uid {
		t.Fatal(bound, err)
	}
	for _, name := range []string{"064_account_identities.sql", "065_github_account_identity.sql", "066_user_nickname.sql", "067_local_user_ai.sql", "068_read_undo.sql", "069_qq_account_identity.sql"} {
		if err := testdb.ExecuteMigrationFile(db, name); err != nil {
			t.Fatalf("replay %s with QQ identity: %v", name, err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM account_identities WHERE user_id=$1`, uid).Scan(&count); err != nil || count != 2 {
		t.Fatal("identity lost", count, err)
	}
	if _, err := repo.ResolveIdentity(ctx, "not-a-provider", "x", "", uid, true); err == nil {
		t.Fatal("unknown provider allowed")
	}
}
