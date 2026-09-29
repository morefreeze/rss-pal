package airouting

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/bytedance/rss-pal/internal/repository/testdb"
	"strings"
	"testing"
)

func TestStoreEncryptedAtomicAndDynamic(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	s := NewStore(db, "01234567890123456789012345678901", "original-key", "https://api.z.ai/api/coding/paas/v4", "glm-5.3-flash")
	ctx := context.Background()
	v, e := s.View(ctx)
	if e != nil || v.Threshold != 10000 || v.Small.Model != "glm-5.3-flash" || v.Large.Model != "glm-5.3" {
		t.Fatalf("%+v %v", v, e)
	}
	u := Update{Revision: v.Revision, Threshold: 10000, Small: v.Small, Large: Selection{"anthropic", "claude-sonnet-4-6"}, Credentials: map[string]CredentialInput{"anthropic": {Endpoint: "default", APIKey: "new-secret"}}}
	v, e = s.Save(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "new-secret") || strings.Contains(string(b), "original-key") {
		t.Fatal("secret leaked")
	}
	var raw string
	if e = db.QueryRow(`SELECT config::text FROM platform_ai_config`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(raw, "new-secret") || strings.Contains(raw, "original-key") {
		t.Fatal("plaintext storage")
	}
	other := NewStore(db, "01234567890123456789012345678901", "original-key", "https://api.z.ai/api/coding/paas/v4", "glm-5.3-flash")
	small, e := other.Resolve(ctx, 10000)
	if e != nil || small.Model != "glm-5.3-flash" || small.APIKey != "original-key" {
		t.Fatal(small.Model, e)
	}
	large, e := other.Resolve(ctx, 10001)
	if e != nil || large.Model != "claude-sonnet-4-6" || large.APIKey != "new-secret" || large.Protocol != "anthropic" {
		t.Fatal(large.Model, e)
	}
	if _, e = s.Save(ctx, u); !errors.Is(e, ErrConflict) {
		t.Fatal("stale update accepted", e)
	}
	u.Revision = v.Revision
	u.Credentials = map[string]CredentialInput{"zai": {Endpoint: "standard"}}
	if _, e = s.Save(ctx, u); e == nil {
		t.Fatal("reused key across endpoints")
	}
}
func TestCipherBindsProvider(t *testing.T) {
	s := NewStore(nil, "test-secret", "", "", "")
	cipher, e := s.seal("openai", "abc")
	if e != nil {
		t.Fatal(e)
	}
	key, e := s.open("openai", cipher)
	if e != nil || key != "abc" {
		t.Fatal(e)
	}
	if _, e = s.open("zai", cipher); e == nil {
		t.Fatal("cross-provider decryption")
	}
}

func TestConfigurationRLSBlocksOrdinaryAppSessions(t *testing.T) {
	db, done := testdb.New(t)
	defer done()
	ctx := context.Background()
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`SET LOCAL ROLE rsspal_app; SET LOCAL app.bypass_rls='false'`); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM platform_ai_config`).Scan(&n); e != nil || n != 0 {
		t.Fatal("config visible to normal session", n, e)
	}
	r, e := tx.Exec(`UPDATE platform_ai_config SET revision=999`)
	if e != nil {
		t.Fatal(e)
	}
	n64, _ := r.RowsAffected()
	if n64 != 0 {
		t.Fatal("normal session changed settings")
	}
	if _, e = tx.Exec(`SET LOCAL app.bypass_rls='true'`); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(`SELECT count(*) FROM platform_ai_config`).Scan(&n); e != nil || n != 1 {
		t.Fatal("worker bypass missing", n, e)
	}
}
