package repository

import (
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"os"
	"testing"
)

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
	for _, name := range []string{"067_local_user_ai.sql", "068_read_undo.sql", "069_qq_account_identity.sql", "070_qq_profile.sql"} {
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
