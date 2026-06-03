# rss-pal backend

## Integration tests

Tests that need a real Postgres use the helper at
`internal/repository/testdb`. It creates a fresh schema per test, runs all
migrations under `backend/migrations/` into that schema, and drops the schema
on cleanup. Schemas are isolated via the connection's `search_path`, so tests
that run in parallel do not collide.

The helper reads the DB connection from `TEST_DB_URL`. If unset, it falls
back to `postgres://postgres:postgres@127.0.0.1:5432/rsspal_test?sslmode=disable`.
If no Postgres is reachable, the helper calls `t.Skipf` and the test is
skipped — it does not fail.

### Bootstrap (one-time)

Reuse the existing dev Postgres container instead of starting a second one
on port 5432. The DB password lives in `.env` under `DB_PASSWORD`.

```bash
docker-compose exec postgres psql -U postgres -c "CREATE DATABASE rsspal_test"
```

(Safe to re-run — ignore `already exists`.)

### Running tests

```bash
export TEST_DB_URL="postgres://postgres:<DB_PASSWORD>@127.0.0.1:5432/rsspal_test?sslmode=disable"
go test ./internal/repository/testdb/...
```
