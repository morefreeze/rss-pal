package repository

import (
	"context"
	"os"
	"testing"

	"github.com/bytedance/rss-pal/internal/repository/testdb"
)

func TestNewNicknamePolicyPreservesExistingNamesAndChoice(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()
	repo := NewUserRepository(db)
	auth := NewAccountAuthRepository(db)
	uid, err := auth.ResolveQQIdentity(context.Background(), "123:restricted-profile", 0, true, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	u, err := repo.FindByID(uid)
	if err != nil || u.QQNickname != "QQ读者" || u.Nickname != "" || u.NeedsNickname {
		t.Fatalf("unsafe QQ default or modification chance consumed: %+v %v", u, err)
	}
	for _, name := range []string{"admin", "root", "ＡＤＭＩＮ_01", "官方客服", "f.u.c.k", "裸聊加微信"} {
		if err := repo.SetNickname(uid, name); err == nil {
			t.Fatalf("restricted nickname accepted: %q", name)
		}
	}
	if err := repo.SetNickname(uid, "普通读者"); err != nil {
		t.Fatalf("rejected names consumed the choice: %v", err)
	}
	if err := repo.SetNickname(uid, "第二个名字"); err != ErrNicknameLocked {
		t.Fatalf("second custom nickname accepted: %v", err)
	}
	// Simulate a nickname saved before this policy. Reads/imports never rewrite it.
	if _, err := db.Exec(`UPDATE users SET nickname='admin' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ResolveQQIdentity(context.Background(), "123:restricted-profile", 0, false, "官方客服", ""); err != nil {
		t.Fatal(err)
	}
	u, err = repo.FindByID(uid)
	if err != nil || u.Nickname != "admin" || u.QQNickname != "QQ读者" {
		t.Fatalf("historical chosen nickname was changed: %+v %v", u, err)
	}
}

func TestNicknameMigrationExistingAccounts(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "065_github_account_identity.sql")
	defer cleanup()
	_, err := db.Exec(`INSERT INTO users(username,password_hash) VALUES ('legacy-name','legacy-hash'),('reader_old','!external')`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/066_user_nickname.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the current repository against the current schema after checking
	// the legacy nickname migration replay. Later profile columns are required.
	for _, name := range []string{"067_local_user_ai.sql", "068_read_undo.sql", "069_qq_account_identity.sql", "070_qq_profile.sql", "071_account_onboarding.sql"} {
		if err := testdb.ExecuteMigrationFile(db, name); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewUserRepository(db)
	legacy, err := repo.FindByUsername("legacy-name")
	if err != nil {
		t.Fatal(err)
	}
	external, err := repo.FindByUsername("reader_old")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.NeedsNickname || !external.NeedsNickname {
		t.Fatal("wrong existing account onboarding")
	}
	if err := repo.SetNickname(external.ID, "已设置昵称"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	external, err = repo.FindByID(external.ID)
	if err != nil {
		t.Fatal(err)
	}
	if external.Nickname != "已设置昵称" || external.NeedsNickname {
		t.Fatal("migration damaged chosen nickname")
	}
}
