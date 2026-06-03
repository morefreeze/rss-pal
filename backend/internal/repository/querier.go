// Package repository contains data-access code for rss-pal.
package repository

import (
	"context"
	"database/sql"
)

// Querier is the minimal subset of *sql.DB used by repositories. Both *sql.DB
// and *sql.Tx satisfy it, so repository code can run either against the global
// pool (worker, startup) or inside a per-request transaction (HTTP handlers).
type Querier interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	Query(query string, args ...interface{}) (*sql.Rows, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRow(query string, args ...interface{}) *sql.Row
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Compile-time assertions that both *sql.DB and *sql.Tx satisfy Querier. If a
// later edit widens or narrows the interface beyond their shared API, this
// fails the build immediately.
var (
	_ Querier = (*sql.DB)(nil)
	_ Querier = (*sql.Tx)(nil)
)
