package repository

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	"github.com/bytedance/rss-pal/internal/config"
	_ "github.com/lib/pq"
)

// NewDB opens a *sql.DB that does NOT bypass RLS. HTTP API handlers must use
// this constructor so the per-request RLSTxMiddleware can pin app.user_id via
// SET LOCAL.
func NewDB(cfg *config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsnFromCfg(cfg))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// NewBypassDB opens a *sql.DB whose connections all have app.bypass_rls=true
// set as a session default. Use only from worker / migration / one-shot CLIs
// that legitimately need to read or write across user boundaries.
// HTTP API handlers must use NewDB so the per-request RLSTxMiddleware can
// pin app.user_id via SET LOCAL.
func NewBypassDB(cfg *config.DatabaseConfig) (*sql.DB, error) {
	dsn := DSNWithBypass(dsnFromCfg(cfg))
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func dsnFromCfg(cfg *config.DatabaseConfig) string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)
}

// DSNWithBypass returns dsn with an additional postgres `options` clause
// that sets app.bypass_rls=true as a session default. Every connection
// opened against the returned DSN inherits the setting at backend startup.
// Use for worker, migration, and admin CLIs that legitimately need to read
// or write across user boundaries.
//
// Supports both URL-form ("postgres://...?sslmode=disable") and key-value
// form ("host=... sslmode=disable") DSNs. Best-effort on malformed input:
// if parsing fails, the original DSN is returned unchanged.
func DSNWithBypass(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		existing := q.Get("options")
		add := "-c app.bypass_rls=true"
		switch {
		case existing == "":
			q.Set("options", add)
		case !strings.Contains(existing, "app.bypass_rls="):
			q.Set("options", existing+" "+add)
		}
		u.RawQuery = q.Encode()
		return u.String()
	}
	// key-value form: append options='-c app.bypass_rls=true' if not present.
	if strings.Contains(dsn, "app.bypass_rls=") {
		return dsn
	}
	sep := " "
	if dsn == "" {
		sep = ""
	}
	return dsn + sep + "options='-c app.bypass_rls=true'"
}
