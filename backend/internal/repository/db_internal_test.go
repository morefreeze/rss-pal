package repository

import (
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
)

func TestDsnFromCfgAs_UsesProvidedCreds(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host: "h", Port: "5432", User: "rsspal_app", Password: "app_pw",
		DBName: "rsspal", SSLMode: "disable",
		AdminUser: "postgres", AdminPassword: "admin_pw",
	}
	runtime := dsnFromCfg(cfg)
	admin := dsnFromCfgAs(cfg, cfg.AdminUser, cfg.AdminPassword)

	if !strings.Contains(runtime, "user=rsspal_app") || !strings.Contains(runtime, "password=app_pw") {
		t.Errorf("runtime DSN missing app creds: %s", runtime)
	}
	if !strings.Contains(admin, "user=postgres") || !strings.Contains(admin, "password=admin_pw") {
		t.Errorf("admin DSN missing admin creds: %s", admin)
	}
}

func TestNewAdminDB_AppliesBypass(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host: "h", Port: "5432", User: "u", Password: "p",
		DBName: "rsspal", SSLMode: "disable",
		AdminUser: "postgres", AdminPassword: "x",
	}
	dsn := DSNWithBypass(dsnFromCfgAs(cfg, cfg.AdminUser, cfg.AdminPassword))
	if !strings.Contains(dsn, "app.bypass_rls=true") {
		t.Errorf("admin DSN missing bypass: %s", dsn)
	}
	if !strings.Contains(dsn, "user=postgres") {
		t.Errorf("admin DSN missing admin user: %s", dsn)
	}
}
