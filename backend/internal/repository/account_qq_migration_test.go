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

func TestQQProfileUpgradeAndPreserveChosenNickname(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "069_qq_account_identity.sql")
	defer cleanup()
	ctx := context.Background()
	repo := NewAccountAuthRepository(db)
	uid, err := repo.ResolveIdentity(ctx, "qq", "123:old-id", "", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := testdb.ExecuteMigrationFile(db, "070_qq_profile.sql"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.ResolveQQIdentity(ctx, "123:old-id", 0, false, "QQ昵称", "https://q.qlogo.cn/qqapp/id/40")
	if err != nil || got != uid {
		t.Fatal("old QQ identity lost", err)
	}
	users := NewUserRepository(db)
	user, err := users.FindByID(uid)
	if err != nil || user.QQNickname != "QQ昵称" || user.Nickname != "" || user.NeedsNickname {
		t.Fatal("bad initial profile", err)
	}
	if err = users.SetNickname(uid, "自定义昵称"); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ResolveQQIdentity(ctx, "123:old-id", 0, false, "更新QQ昵称", ""); err != nil {
		t.Fatal(err)
	}
	if err = testdb.ExecuteMigrationFile(db, "070_qq_profile.sql"); err != nil {
		t.Fatal(err)
	}
	user, err = users.FindByID(uid)
	if err != nil || user.Nickname != "自定义昵称" || user.QQNickname != "更新QQ昵称" {
		t.Fatal("chosen nickname or profile damaged", err)
	}
	other, err := repo.ResolveIdentity(ctx, "email", "other@example.com", "other@example.com", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ResolveQQIdentity(ctx, "123:old-id", other, true, "must-not-save", ""); err == nil {
		t.Fatal("foreign identity bound")
	}
	user, _ = users.FindByID(uid)
	if user.QQNickname != "更新QQ昵称" {
		t.Fatal("failed binding changed metadata")
	}
}
