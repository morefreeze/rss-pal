package repository

import (
	"database/sql"
	"fmt"
)

// txOrBegin returns (tx, commit, rollback) for the current Querier. If the
// Querier is already a *sql.Tx (e.g. RLS middleware wrapped the request in
// one), the returned tx is the same outer tx and commit/rollback are no-ops —
// the outer middleware owns the lifecycle. Otherwise it opens a fresh inner
// tx on the *sql.DB and returns real commit/rollback funcs.
//
// CALLERS MUST PROPAGATE any error returned by the operation up to the
// request handler. With an outer tx in flight, swallowing the error commits
// partial work along with the rest of the request. If a caller intentionally
// ignores the error (e.g. best-effort cache write), it MUST still log the
// error and ensure no further state is mutated under the assumption that
// the inner operation succeeded.
func txOrBegin(q Querier) (Querier, func() error, func() error, error) {
	if tx, ok := q.(*sql.Tx); ok {
		noop := func() error { return nil }
		return tx, noop, noop, nil
	}
	db, ok := q.(*sql.DB)
	if !ok {
		return nil, nil, nil, fmt.Errorf("txOrBegin: Querier is neither *sql.Tx nor *sql.DB")
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, nil, nil, err
	}
	return tx, tx.Commit, tx.Rollback, nil
}
