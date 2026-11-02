package main

import (
	"github.com/bytedance/rss-pal/internal/airouting"
	"github.com/bytedance/rss-pal/internal/config"
	"net/url"
	"strings"
	"testing"
)

func TestReadOnlyDSNAndEvaluatorCredentialBoundary(t *testing.T) {
	dsn := readOnlyDSN(config.DatabaseConfig{Host: "db", Port: "5432", User: "reader", Password: "test:p@ss", DBName: "rsspal", SSLMode: "disable"})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	if password != "test:p@ss" || u.Query().Get("options") != "-c default_transaction_read_only=on" {
		t.Fatal("invalid readonly DSN")
	}
	t.Setenv("DB_PASSWORD", "db-only-secret")
	t.Setenv("SHARE_SECRET", "encryption-secret")
	t.Setenv("SUMMARY_EVAL_JUDGE_API_KEY", "unrelated-secret")
	env := strings.Join(providerEnv(airouting.Target{APIKey: "selected-provider-key", BaseURL: "https://example.com", Model: "model"}), "\n")
	for _, secret := range []string{"db-only-secret", "encryption-secret", "unrelated-secret"} {
		if strings.Contains(env, secret) {
			t.Fatal("evaluator inherited unrelated credential")
		}
	}
	if !strings.Contains(env, "AI_API_KEY=selected-provider-key") {
		t.Fatal("selected provider not passed")
	}
}
