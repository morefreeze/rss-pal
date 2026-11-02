// summary-eval-site resolves the site's current article model with a read-only
// DB connection, then launches the DB-free evaluator with credentials in memory.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"time"

	"github.com/bytedance/rss-pal/internal/airouting"
	"github.com/bytedance/rss-pal/internal/config"
	"github.com/lib/pq"
)

func main() { os.Exit(run()) }

func run() int {
	flags := flag.NewFlagSet("summary-eval-site", flag.ContinueOnError)
	runes := flags.Int("source-runes", 1, "source length used to resolve the article-model route")
	binary := flags.String("binary", "summary-eval", "path to DB-free evaluator")
	inspect := flags.Bool("inspect", false, "print only non-sensitive route metadata without model calls")
	if flags.Parse(os.Args[1:]) != nil || *runes < 1 {
		return 1
	}
	cfg := config.Load()
	db, err := sql.Open("postgres", readOnlyDSN(cfg.Database))
	if err != nil {
		return fail("cannot initialize read-only model configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store := airouting.NewStore(db, cfg.Share.Secret, cfg.Claude.APIKey, cfg.Claude.BaseURL, cfg.Claude.Model)
	before, err := store.View(ctx)
	if err != nil {
		return fail("cannot read model configuration revision")
	}
	target, err := store.Resolve(ctx, *runes)
	if err != nil {
		var pg *pq.Error
		if errors.As(err, &pg) {
			return fail("cannot resolve configured article model: database code " + string(pg.Code))
		}
		return fail("cannot resolve configured article model: configuration or credential unavailable")
	}
	view, err := store.View(ctx)
	if err != nil {
		return fail("cannot read model configuration revision")
	}
	if before.Revision != view.Revision {
		return fail("model configuration changed during resolution; retry before making calls")
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"provider": target.Provider, "model": target.Model, "protocol": target.Protocol, "large": target.Large, "source_runes": *runes, "threshold": view.Threshold, "configuration_revision": view.Revision})
	if *inspect {
		return 0
	}
	if target.Protocol != "openai" && target.Protocol != "" {
		return fail("configured provider requires native protocol support; no calls made")
	}
	command := exec.Command(*binary, flags.Args()...)
	// Do not give the evaluator the database password, encryption key, unrelated
	// provider credentials, or other application secrets.
	command.Env = providerEnv(target)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return fail("cannot launch evaluator")
	}
	return 0
}

func readOnlyDSN(db config.DatabaseConfig) string {
	if db.AdminUser != "" {
		db.User = db.AdminUser
		db.Password = db.AdminPassword
	}
	u := url.URL{Scheme: "postgres", User: url.UserPassword(db.User, db.Password), Host: net.JoinHostPort(db.Host, db.Port), Path: "/" + db.DBName}
	q := url.Values{"sslmode": {db.SSLMode}, "options": {"-c default_transaction_read_only=on"}, "connect_timeout": {"10"}}
	u.RawQuery = q.Encode()
	return u.String()
}

func providerEnv(target airouting.Target) []string {
	env := []string{}
	for _, key := range []string{"PATH", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "TZ"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env, "AI_API_KEY="+target.APIKey, "AI_BASE_URL="+target.BaseURL, "AI_MODEL="+target.Model)
}

func fail(message string) int { fmt.Fprintln(os.Stderr, message); return 1 }
