package repository_test

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
)

func TestDSNWithBypass(t *testing.T) {
	cases := []struct {
		name string
		in   string
		// wantOptionsContains is a substring that must appear in the parsed
		// `options` value (URL form) or in the raw DSN (key-value form).
		wantContains string
	}{
		{
			name:         "url no options",
			in:           "postgres://u:p@h:5432/d?sslmode=disable",
			wantContains: "app.bypass_rls=true",
		},
		{
			name:         "url with other options",
			in:           "postgres://u:p@h:5432/d?sslmode=disable&options=-c+statement_timeout%3D5000",
			wantContains: "app.bypass_rls=true",
		},
		{
			name:         "url already has bypass",
			in:           "postgres://u:p@h:5432/d?options=-c+app.bypass_rls%3Dtrue",
			wantContains: "app.bypass_rls=true",
		},
		{
			name:         "postgresql scheme",
			in:           "postgresql://u:p@h:5432/d?sslmode=disable",
			wantContains: "app.bypass_rls=true",
		},
		{
			name:         "keyvalue no options",
			in:           "host=h port=5432 user=u password=p dbname=d sslmode=disable",
			wantContains: "options='-c app.bypass_rls=true'",
		},
		{
			name:         "keyvalue with bypass already",
			in:           "host=h options='-c app.bypass_rls=true'",
			wantContains: "app.bypass_rls=true",
		},
		{
			name:         "empty",
			in:           "",
			wantContains: "options='-c app.bypass_rls=true'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := repository.DSNWithBypass(tc.in)

			// For URL form, decode the options query param and check
			// the decoded value, since url.Values.Encode may percent-
			// encode `=` differently across Go versions.
			if strings.HasPrefix(got, "postgres://") || strings.HasPrefix(got, "postgresql://") {
				u, err := url.Parse(got)
				if err != nil {
					t.Fatalf("parse result %q: %v", got, err)
				}
				opts := u.Query().Get("options")
				if !strings.Contains(opts, "app.bypass_rls=true") {
					t.Fatalf("DSNWithBypass(%q) options=%q; want substring app.bypass_rls=true", tc.in, opts)
				}
			} else {
				if !strings.Contains(got, tc.wantContains) {
					t.Fatalf("DSNWithBypass(%q) = %q; want substring %q", tc.in, got, tc.wantContains)
				}
			}

			// Never duplicate the setting.
			if strings.Count(got, "app.bypass_rls=") > 1 {
				t.Fatalf("duplicated bypass setting in %q", got)
			}
		})
	}
}

// testRepoCfg parses TEST_DB_URL into a *config.DatabaseConfig. Returns nil
// if the env var is unset (caller should skip).
func testRepoCfg(t *testing.T) *config.DatabaseConfig {
	t.Helper()
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		return nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DB_URL: %v", err)
	}
	pw, _ := u.User.Password()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	dbname := strings.TrimPrefix(u.Path, "/")
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}
	return &config.DatabaseConfig{
		Host:     u.Hostname(),
		Port:     port,
		User:     u.User.Username(),
		Password: pw,
		DBName:   dbname,
		SSLMode:  sslmode,
	}
}

func TestNewBypassDB_SessionDefaultIsTrue(t *testing.T) {
	cfg := testRepoCfg(t)
	if cfg == nil {
		t.Skip("TEST_DB_URL unset")
	}
	db, err := repository.NewBypassDB(cfg)
	if err != nil {
		t.Skipf("connect: %v", err)
	}
	defer db.Close()

	var got string
	if err := db.QueryRow(`SELECT current_setting('app.bypass_rls', true)`).Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if got != "true" {
		t.Fatalf("want app.bypass_rls=true session default, got %q", got)
	}
}

func TestNewDB_NoBypassByDefault(t *testing.T) {
	cfg := testRepoCfg(t)
	if cfg == nil {
		t.Skip("TEST_DB_URL unset")
	}
	db, err := repository.NewDB(cfg)
	if err != nil {
		t.Skipf("connect: %v", err)
	}
	defer db.Close()

	var got sql.NullString
	if err := db.QueryRow(`SELECT current_setting('app.bypass_rls', true)`).Scan(&got); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// Unset GUC returns NULL (or empty string) with missing_ok=true.
	if got.Valid && got.String == "true" {
		t.Fatalf("NewDB unexpectedly has app.bypass_rls=true session default")
	}
}
