package repository

import (
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"os"
	"testing"
)

func TestLocalAIMigrationErasesLegacyKeysAndRejectsNewKeys(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "066_user_nickname.sql")
	defer cleanup()
	var user int
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('local-ai-migration','x') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_ai_configs(user_id,api_key,base_url,model) VALUES($1,'old-test-secret','https://example.test','test')`, user); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/067_local_user_ai.sql")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := db.Exec(string(migration)); err != nil {
			t.Fatal(err)
		}
	}
	var key string
	if err := db.QueryRow(`SELECT api_key FROM user_ai_configs WHERE user_id=$1`, user).Scan(&key); err != nil || key != "" {
		t.Fatalf("key erased=%v err=%v", key == "", err)
	}
	if _, err := db.Exec(`UPDATE user_ai_configs SET api_key='must-be-rejected' WHERE user_id=$1`, user); err == nil {
		t.Fatal("database accepted a new user credential")
	}
}
