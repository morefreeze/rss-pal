# Single Article External Sharing Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a signed-in RSS Pal user create multiple independently expiring or revocable immutable article snapshots that an unauthenticated visitor can read in the full reader UI.

**Architecture:** Store one versioned JSONB snapshot per share in a new non-RLS `article_shares` table, authorize owner operations explicitly inside the existing request RLS transaction, and expose snapshots through HMAC-signed URLs. Reuse presentation-only reader components for the public page, gate local PDF assets with the same share token, and preserve old eight-character links for a migration-time 30-day grace period using SHA-256 digests only.

**Tech Stack:** Go 1.24, Gin, PostgreSQL 15, React 18, TypeScript, React Router 6, Axios, Vitest, Testing Library.

---

## Execution Routing

Use **Subagent-Driven Development** for execution. The plan has independently reviewable security, persistence/API, public-media, and frontend/auth-intent boundaries; each task must still land sequentially because later tasks depend on types and routes introduced earlier.

Execute from a new isolated worktree and implementation branch created from `codex/external-article-sharing` so the untracked backup files in `/Users/bytedance/mygit/rss-pal` remain untouched. Suggested branch: `codex/external-article-sharing-impl`.

## File Map

Backend files to create:

- `backend/internal/sharetoken/signer.go` — public ID generation and HMAC token signing/parsing.
- `backend/internal/sharetoken/signer_test.go` — token format, tamper and secret validation tests.
- `backend/migrations/039_article_shares.sql` — new table, indexes, legacy snapshot migration and old table removal.
- `backend/internal/model/article_share.go` — persisted share and versioned public snapshot types.
- `backend/internal/repository/share_test.go` — snapshot immutability, lifecycle and owner-filter tests.
- `backend/internal/api/share_test.go` — authenticated and public endpoint contract tests.
- `backend/internal/api/share_rate_limit.go` — route-scoped fixed-window visitor limiter.
- `backend/internal/api/share_rate_limit_test.go` — limiter and trusted client-IP behavior tests.

Backend files to modify:

- `backend/internal/config/config.go` — add `ShareConfig` and load `SHARE_SECRET`.
- `backend/internal/config/share_test.go` — pin share-secret loading behavior.
- `backend/internal/repository/share.go` — replace one-token-per-article storage with immutable multi-share operations.
- `backend/internal/model/ai_config.go` — remove obsolete `ShareToken` after callers move.
- `backend/internal/api/share.go` — authenticated CRUD, signed public lookup, response sanitization and legacy lookup.
- `backend/internal/api/article_images.go` — expose an internal method that serves a validated article image path.
- `backend/cmd/server/main.go` — construct signer/handlers and register new routes and limiter.
- `docker-compose.yml` — require `SHARE_SECRET` and apply migration 039 in `status-migrate`.
- `.env.example` — document the new independent secret.
- `README.md` — update share API documentation.

Frontend files to create:

- `frontend/src/components/ShareDialog.tsx` — create, list, copy and revoke shares.
- `frontend/src/components/PublicArticleReader.tsx` — presentation-only full article renderer.
- `frontend/src/utils/authIntent.ts` — validate and preserve post-auth destinations.
- `frontend/test/ShareDialog.test.tsx` — share-management UI contracts.
- `frontend/test/SharePage.test.tsx` — public reader, errors, media and CTA contracts.
- `frontend/test/AuthIntentRoutes.test.tsx` — login/register intent preservation and feed prefill.

Frontend files to modify:

- `frontend/src/api/client.ts` — new share types and authenticated API methods.
- `frontend/src/pages/ArticlePage.tsx` — replace inline share handlers/section with `ShareDialog`.
- `frontend/src/pages/SharePage.tsx` — fetch and render the immutable snapshot.
- `frontend/src/pages/LoginPage.tsx` — preserve validated auth intent and navigate after login.
- `frontend/src/pages/RegisterPage.tsx` — preserve validated auth intent and navigate after registration.
- `frontend/src/pages/FeedListPage.tsx` — prefill and preview the source page URL after authentication.
- `frontend/src/App.tsx` — keep the public route and ensure auth pages receive the query string naturally.
- `frontend/src/index.css` — focused dialog and public-reader styles.

## Task 1: Signed Share Tokens and Required Configuration

**Files:**

- Create: `backend/internal/sharetoken/signer.go`
- Create: `backend/internal/sharetoken/signer_test.go`
- Create: `backend/internal/config/share_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `.env.example`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write failing signer tests**

Create tests that pin the external format and all rejection paths:

```go
package sharetoken

import (
    "strings"
    "testing"
)

func TestSignerRoundTrip(t *testing.T) {
    signer, err := NewSigner("0123456789abcdef0123456789abcdef")
    if err != nil { t.Fatal(err) }
    id, err := NewPublicID()
    if err != nil { t.Fatal(err) }
    token := signer.Sign(id)
    got, legacy, err := signer.Parse(token)
    if err != nil { t.Fatal(err) }
    if legacy || got != id { t.Fatalf("Parse() = %q,%v want %q,false", got, legacy, id) }
}

func TestSignerRejectsTamperingAndWeakSecret(t *testing.T) {
    if _, err := NewSigner("short"); err == nil { t.Fatal("weak secret accepted") }
    signer, _ := NewSigner("0123456789abcdef0123456789abcdef")
    id, _ := NewPublicID()
    token := signer.Sign(id)
    token = token[:len(token)-1] + map[bool]string{true: "A", false: "B"}[strings.HasSuffix(token, "B")]
    if _, _, err := signer.Parse(token); err == nil { t.Fatal("tampered token accepted") }
    if _, _, err := signer.Parse("v2_"+id+"_bad"); err == nil { t.Fatal("unknown version accepted") }
}

func TestSignerRecognizesLegacyShapeWithoutTrustingIt(t *testing.T) {
    signer, _ := NewSigner("0123456789abcdef0123456789abcdef")
    got, legacy, err := signer.Parse("aB3dE6gH")
    if err != nil || !legacy || got != "aB3dE6gH" { t.Fatalf("legacy parse = %q,%v,%v", got, legacy, err) }
}
```

- [ ] **Step 2: Run the signer test and verify RED**

Run: `cd backend && go test ./internal/sharetoken -count=1`

Expected: FAIL because package `internal/sharetoken` does not exist.

- [ ] **Step 3: Implement the signer with standard-library crypto only**

Implement this public surface in `signer.go`:

```go
package sharetoken

type Signer struct { secret []byte }

func NewSigner(secret string) (*Signer, error)
func NewPublicID() (string, error) // 16 crypto/rand bytes, 32 lowercase hex chars
func (s *Signer) Sign(publicID string) string // v1_<id>_<base64url-hmac>
func (s *Signer) Parse(token string) (value string, legacy bool, err error)
func LegacyDigest(token string) string // lowercase SHA-256 hex
```

`Parse` must validate the exact three-part v1 shape, validate a 32-character hex ID, recompute HMAC-SHA256 over `v1:` plus the ID, decode the signature with `base64.RawURLEncoding`, and compare with `hmac.Equal`. It may recognize exactly eight ASCII alphanumeric characters as legacy, but it must not treat that shape as authenticated; repository lookup of `LegacyDigest` is still required.

- [ ] **Step 4: Add required configuration tests and implementation**

Add `Share ShareConfig` to `config.Config`:

```go
type ShareConfig struct { Secret string }

// in Load()
Share: ShareConfig{Secret: getEnv("SHARE_SECRET", "")},
```

Test with `t.Setenv("SHARE_SECRET", "share-secret-for-test")` that `Load().Share.Secret` matches. Add `SHARE_SECRET=change_me_to_a_separate_32_byte_random_string` to `.env.example` and add this required Compose environment entry to the API service only:

```yaml
SHARE_SECRET: ${SHARE_SECRET:?SHARE_SECRET required in .env}
```

- [ ] **Step 5: Run focused tests and verify GREEN**

Run: `cd backend && go test ./internal/sharetoken ./internal/config -count=1`

Expected: PASS.

- [ ] **Step 6: Commit the token/config boundary**

```bash
git add backend/internal/sharetoken backend/internal/config/config.go backend/internal/config/share_test.go .env.example docker-compose.yml
git commit -m "feat: add signed share tokens"
```

## Task 2: Immutable Share Persistence and Legacy Migration

**Files:**

- Create: `backend/migrations/039_article_shares.sql`
- Create: `backend/internal/model/article_share.go`
- Create: `backend/internal/repository/share_test.go`
- Modify: `backend/internal/repository/share.go`
- Modify: `backend/internal/model/ai_config.go`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Write failing repository tests**

Use `repository/testdb.New(t)` to seed two users, two owned feeds and ready articles. Tests must assert:

```go
type shareRepoFixture struct {
    db      *sql.DB
    repo    *ShareRepository
    userA   int
    userB   int
    article *model.Article
}

func newShareRepoFixture(t *testing.T) *shareRepoFixture {
    t.Helper()
    db, cleanup := testdb.New(t)
    t.Cleanup(cleanup)
    f := &shareRepoFixture{db: db, repo: NewShareRepository(db)}
    for target, username := range map[*int]string{&f.userA: "share-a", &f.userB: "share-b"} {
        if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES($1,'x') RETURNING id`, username).Scan(target); err != nil { t.Fatal(err) }
    }
    var feedID int
    if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://feed.example/rss','Feed A',$1) RETURNING id`, f.userA).Scan(&feedID); err != nil { t.Fatal(err) }
    f.article = &model.Article{FeedID: feedID, FeedTitle: "Feed A", Title: "Original", URL: "https://feed.example/post", Content: "Body", SummaryBrief: "Brief", SummaryDetailed: "Detail", WordCount: 10, ReadingMinutes: 2, ProcessingState: "ready"}
    if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content,summary_brief,summary_detailed,processing_state) VALUES($1,$2,$3,$4,$5,$6,'ready') RETURNING id`, feedID, f.article.Title, f.article.URL, f.article.Content, f.article.SummaryBrief, f.article.SummaryDetailed).Scan(&f.article.ID); err != nil { t.Fatal(err) }
    return f
}

func TestShareRepositoryCreateListRevokeAndSnapshotImmutability(t *testing.T) {
    f := newShareRepoFixture(t)
    now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
    first, err := f.repo.Create(f.article, f.userA, "11111111111111111111111111111111", nil, now)
    if err != nil { t.Fatal(err) }
    second, err := f.repo.Create(f.article, f.userA, "22222222222222222222222222222222", ptrTime(now.Add(time.Hour)), now)
    if err != nil || first.PublicID == second.PublicID { t.Fatalf("second=%+v err=%v", second, err) }
    if _, err := f.db.Exec(`UPDATE articles SET title='Changed', content='Changed body' WHERE id=$1`, f.article.ID); err != nil { t.Fatal(err) }
    got, err := f.repo.GetActiveByPublicID(first.PublicID, now)
    if err != nil || got.Snapshot.Title != "Original" || got.Snapshot.Content != "Body" { t.Fatalf("got=%+v err=%v", got, err) }
    if _, err := f.repo.Revoke(first.PublicID, f.article.ID, f.userA, now); err != nil { t.Fatal(err) }
    got, err = f.repo.GetActiveByPublicID(first.PublicID, now)
    if err != nil || got != nil { t.Fatalf("revoked got=%+v err=%v", got, err) }
}

func TestShareRepositoryOwnerFiltersListAndRevoke(t *testing.T) {
    f := newShareRepoFixture(t)
    now := time.Now().UTC()
    row, err := f.repo.Create(f.article, f.userA, "33333333333333333333333333333333", nil, now)
    if err != nil { t.Fatal(err) }
    rows, err := f.repo.List(f.article.ID, f.userB, now)
    if err != nil || len(rows) != 0 { t.Fatalf("rows=%+v err=%v", rows, err) }
    if got, err := f.repo.Revoke(row.PublicID, f.article.ID, f.userB, now); err != nil || got != nil { t.Fatalf("got=%+v err=%v", got, err) }
}

func TestShareRepositoryExpirationAndLegacyDigest(t *testing.T) {
    f := newShareRepoFixture(t)
    now := time.Now().UTC()
    row, err := f.repo.Create(f.article, f.userA, "44444444444444444444444444444444", ptrTime(now.Add(time.Minute)), now)
    if err != nil { t.Fatal(err) }
    got, err := f.repo.GetActiveByPublicID(row.PublicID, now.Add(time.Minute))
    if err != nil || got != nil { t.Fatalf("expired got=%+v err=%v", got, err) }
    legacyExpiry := now.Add(30 * 24 * time.Hour)
    if _, err := f.db.Exec(`UPDATE article_shares SET legacy_token_digest=$1, expires_at=$2 WHERE public_id=$3`, sharetoken.LegacyDigest("aB3dE6gH"), legacyExpiry, row.PublicID); err != nil { t.Fatal(err) }
    got, err = f.repo.GetActiveByLegacyDigest(sharetoken.LegacyDigest("aB3dE6gH"), now)
    if err != nil || got == nil { t.Fatalf("legacy got=%+v err=%v", got, err) }
    got, err = f.repo.GetActiveByLegacyDigest(sharetoken.LegacyDigest("aB3dE6gH"), legacyExpiry)
    if err != nil || got != nil { t.Fatalf("expired legacy got=%+v err=%v", got, err) }
}

func ptrTime(v time.Time) *time.Time { return &v }
```

Assert the decoded public snapshot contains title, URL, feed title, content, both summaries, word/reading values, media fields and image dimensions, but has no JSON keys named `created_by`, `editor_note`, `feed_id`, `manual_tags` or `is_read`.

- [ ] **Step 2: Run repository tests and verify RED**

Run: `cd backend && go test ./internal/repository -run 'TestShareRepository' -count=1`

Expected: FAIL because the new model and repository operations are undefined.

- [ ] **Step 3: Add model types**

Create `article_share.go` with one persisted model and one explicit wire-safe snapshot:

```go
package model

import "time"

type ArticleShareSnapshot struct {
    Title                string            `json:"title"`
    URL                  string            `json:"url"`
    FeedTitle            string            `json:"feed_title,omitempty"`
    PublishedAt          *time.Time        `json:"published_at,omitempty"`
    WordCount            int               `json:"word_count,omitempty"`
    ReadingMinutes       int               `json:"reading_minutes,omitempty"`
    SummaryBrief         string            `json:"summary_brief,omitempty"`
    SummaryDetailed      string            `json:"summary_detailed,omitempty"`
    Content              string            `json:"content"`
    MediaURL             string            `json:"media_url,omitempty"`
    MediaType            string            `json:"media_type,omitempty"`
    MediaDurationSeconds int               `json:"media_duration_seconds,omitempty"`
    ImageDimensions      map[string][2]int `json:"image_dimensions,omitempty"`
    SnapshottedAt        time.Time         `json:"snapshotted_at"`
}

type ArticleShare struct {
    PublicID          string
    ArticleID         int
    CreatedBy         int
    SnapshotVersion   int
    Snapshot          ArticleShareSnapshot
    ExpiresAt         *time.Time
    RevokedAt         *time.Time
    LegacyTokenDigest *string
    CreatedAt         time.Time
}
```

Remove the obsolete `model.ShareToken` only after `repository/share.go` no longer references it.

- [ ] **Step 4: Add migration 039**

The migration must be idempotent for a partially completed production run and must use `TIMESTAMPTZ`:

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS article_shares (
    public_id VARCHAR(32) PRIMARY KEY,
    article_id INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    created_by INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    snapshot_version SMALLINT NOT NULL DEFAULT 1 CHECK (snapshot_version = 1),
    snapshot JSONB NOT NULL,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    legacy_token_digest CHAR(64),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at IS NULL OR expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS idx_article_shares_owner_article_created
    ON article_shares (created_by, article_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_article_shares_legacy_digest
    ON article_shares (legacy_token_digest)
    WHERE legacy_token_digest IS NOT NULL;
```

Before copying, use a `DO` block to raise if any old row has null `created_by` or no matching article/feed. Guard the whole legacy copy with `to_regclass('share_tokens') IS NOT NULL` so the repository test harness's temporary schema works, and execute the copy dynamically so PostgreSQL does not parse a reference to a table already dropped during a rerun:

```sql
DO $$
BEGIN
    IF to_regclass('share_tokens') IS NOT NULL THEN
        IF EXISTS (
            SELECT 1 FROM share_tokens st
            LEFT JOIN articles a ON a.id = st.article_id
            LEFT JOIN feeds f ON f.id = a.feed_id
            WHERE st.created_by IS NULL OR a.id IS NULL OR f.id IS NULL
        ) THEN
            RAISE EXCEPTION 'share_tokens contains rows that cannot be migrated safely';
        END IF;

        EXECUTE $copy$
            INSERT INTO article_shares (
                public_id, article_id, created_by, snapshot_version, snapshot,
                expires_at, legacy_token_digest, created_at
            )
            SELECT encode(gen_random_bytes(16), 'hex'), st.article_id, st.created_by, 1,
                   jsonb_build_object(
                       'title', a.title, 'url', a.url,
                       'feed_title', COALESCE(f.title, ''),
                       'published_at', a.published_at,
                       'word_count', COALESCE(a.word_count, 0),
                       'reading_minutes', COALESCE(a.reading_minutes, 0),
                       'summary_brief', COALESCE(a.summary_brief, ''),
                       'summary_detailed', COALESCE(a.summary_detailed, ''),
                       'content', COALESCE(a.content, ''),
                       'media_url', COALESCE(a.media_url, ''),
                       'media_type', COALESCE(a.media_type, ''),
                       'media_duration_seconds', COALESCE(a.media_duration_seconds, 0),
                       'image_dimensions', COALESCE(a.image_dimensions, '{}'::jsonb),
                       'snapshotted_at', NOW()
                   ),
                   NOW() + INTERVAL '30 days',
                   encode(digest(st.token, 'sha256'), 'hex'),
                   st.created_at AT TIME ZONE current_setting('TIMEZONE')
              FROM share_tokens st
              JOIN articles a ON a.id = st.article_id
              JOIN feeds f ON f.id = a.feed_id
            ON CONFLICT (legacy_token_digest)
                WHERE legacy_token_digest IS NOT NULL DO NOTHING
        $copy$;
        DROP TABLE share_tokens;
    END IF;
END
$$;
```

Add comments explaining that `article_shares` intentionally has no RLS because public token resolution happens before `app.user_id` is set; every owner endpoint must filter `created_by` and create only from an RLS-authorized article row.

- [ ] **Step 5: Implement repository operations**

Keep the existing `Querier + WithCtx` pattern and implement:

```go
func SnapshotFromArticle(a *model.Article, now time.Time) model.ArticleShareSnapshot
func (r *ShareRepository) Create(article *model.Article, createdBy int, publicID string, expiresAt *time.Time, now time.Time) (*model.ArticleShare, error)
func (r *ShareRepository) List(articleID, createdBy int, now time.Time) ([]model.ArticleShare, error)
func (r *ShareRepository) Revoke(publicID string, articleID, createdBy int, now time.Time) (*model.ArticleShare, error)
func (r *ShareRepository) GetActiveByPublicID(publicID string, now time.Time) (*model.ArticleShare, error)
func (r *ShareRepository) GetActiveByLegacyDigest(digest string, now time.Time) (*model.ArticleShare, error)
```

`Create` marshals only `ArticleShareSnapshot`. `GetActive*` includes `revoked_at IS NULL AND (expires_at IS NULL OR expires_at > $now)`. `Revoke` uses `UPDATE ... WHERE public_id=$1 AND article_id=$2 AND created_by=$3 RETURNING ...` and `COALESCE(revoked_at, $now)` for idempotence.

- [ ] **Step 6: Wire migration 039 into the one-shot Compose migration service**

Append `psql -v ON_ERROR_STOP=1 -f /migrations/039_article_shares.sql` to the existing `status-migrate` command after migration 038. Do not add a second migration container.

- [ ] **Step 7: Run migration and repository tests**

Run: `cd backend && go test ./internal/repository -run 'TestShareRepository|TestMigrations' -count=1`

Expected: PASS. If the local PostgreSQL test dependency is unavailable, the repository integration tests must report their established explicit skip; unit-only success must not be reported as database verification.

- [ ] **Step 8: Commit persistence**

```bash
git add backend/migrations/039_article_shares.sql backend/internal/model/article_share.go backend/internal/model/ai_config.go backend/internal/repository/share.go backend/internal/repository/share_test.go docker-compose.yml
git commit -m "feat: persist immutable article shares"
```

## Task 3: Authenticated Share Management API and RLS Authorization

**Files:**

- Create: `backend/internal/api/share_test.go`
- Modify: `backend/internal/api/share.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing authenticated API tests**

Build a minimal Gin router using `rsspal_app`, `AuthMiddleware`, and `RLSTxMiddleware`, following `backend/internal/api/rls_http_leak_test.go`. Cover:

```go
func TestCreateShareRequiresVisibleReadyArticle(t *testing.T) {
    f := newShareAPIFixture(t)
    if got := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, `{"expires_at":null}`); got.Code != http.StatusCreated { t.Fatalf("owner status=%d body=%s", got.Code, got.Body.String()) }
    if got := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userB, `{"expires_at":null}`); got.Code != http.StatusNotFound { t.Fatalf("other status=%d body=%s", got.Code, got.Body.String()) }
    f.setArticleState(t, "processing", "")
    if got := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, `{"expires_at":null}`); got.Code != http.StatusConflict { t.Fatalf("processing status=%d", got.Code) }
}

func TestCreateShareValidatesExpiryAndCreatesMultipleRows(t *testing.T) {
    f := newShareAPIFixture(t)
    past := `{"expires_at":"2026-09-08T11:59:59Z"}`
    if got := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, past); got.Code != http.StatusBadRequest { t.Fatalf("past status=%d", got.Code) }
    a := f.createShare(t, f.userA, `{"expires_at":null}`)
    b := f.createShare(t, f.userA, `{"expires_at":"2026-10-08T12:00:00Z"}`)
    if a.ID == b.ID || a.URL == b.URL { t.Fatalf("shares must be independent: a=%+v b=%+v", a, b) }
}

func TestListAndRevokeSharesAreOwnerScoped(t *testing.T) {
    f := newShareAPIFixture(t)
    row := f.createShare(t, f.userA, `{"expires_at":null}`)
    if got := f.request(t, http.MethodGet, f.articlePath("/shares"), f.userB, ""); got.Code != http.StatusNotFound { t.Fatalf("other list status=%d", got.Code) }
    path := f.articlePath("/shares/" + row.ID)
    if got := f.request(t, http.MethodDelete, path, f.userB, ""); got.Code != http.StatusNotFound { t.Fatalf("other revoke status=%d", got.Code) }
    for i := 0; i < 2; i++ {
        got := f.request(t, http.MethodDelete, path, f.userA, "")
        if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"status":"revoked"`) { t.Fatalf("attempt %d status=%d body=%s", i, got.Code, got.Body.String()) }
    }
}
```

In the same test file, implement `newShareAPIFixture`, `request`, `createShare`, `articlePath`, and `setArticleState` as concrete helpers around a migrated `rsspal_app` pool, fixed clock `2026-09-08T12:00:00Z`, signed JWTs, and explicit SQL seeding. Do not mock the repository in these authorization tests.

Decode responses and assert the list exposes `id`, `url`, `created_at`, `expires_at`, `status`, and `legacy`, but never the snapshot body or `created_by`.

- [ ] **Step 2: Run the API tests and verify RED**

Run: `cd backend && go test ./internal/api -run 'Test(Create|List|Revoke)Share' -count=1`

Expected: FAIL because new routes and handler methods are undefined.

- [ ] **Step 3: Replace the authenticated handler surface**

Construct `ShareHandler` with repository, article repository, signer, clock, and PDF image handler:

```go
type ShareHandler struct {
    shares   *repository.ShareRepository
    articles *repository.ArticleRepository
    signer   *sharetoken.Signer
    now      func() time.Time
    images   *ArticleImageHandler
}

func (h *ShareHandler) Create(c *gin.Context)
func (h *ShareHandler) List(c *gin.Context)
func (h *ShareHandler) Revoke(c *gin.Context)
```

`Create` parses RFC3339 `expires_at`, rejects non-future values, calls `h.articles.WithCtx(c).GetByID(id, userID)`, maps `sql.ErrNoRows` to 404, requires `processing_state` to be empty or `ready` and `strings.TrimSpace(content) != ""`, then calls the share repository bound to the same context transaction. Return 201.

`List` and `Revoke` must perform the same RLS-bound `ArticleRepository.GetByID` lookup before touching `article_shares`; this is what makes an inaccessible numeric article ID return 404 instead of an empty list that reveals the resource shape.

In `main.go`, construct the signer before the handlers and fail startup for a missing or weak secret:

```go
shareSigner, err := sharetoken.NewSigner(cfg.Share.Secret)
if err != nil {
    log.Fatalf("invalid SHARE_SECRET: %v", err)
}
shareHandler := api.NewShareHandler(shareRepo, articleRepo, shareSigner, pdfImgHandler, time.Now)
```

Move the existing `pdfImgHandler := api.NewArticleImageHandler(...)` construction above `NewShareHandler` and reuse the same instance for the existing article-image route; do not create two handlers with different base directories.

Register:

```go
apiGroup.POST("/articles/:id/shares", shareHandler.Create)
apiGroup.GET("/articles/:id/shares", shareHandler.List)
apiGroup.DELETE("/articles/:id/shares/:share_id", shareHandler.Revoke)
```

Remove the old `POST /articles/:id/share` registration.

- [ ] **Step 4: Run focused API and isolation tests**

Run: `cd backend && go test ./internal/api -run 'Test(Create|List|Revoke)Share|TestRLSHTTP' -count=1`

Expected: PASS, including the numeric cross-user article-ID attempt.

- [ ] **Step 5: Commit authenticated API**

```bash
git add backend/internal/api/share.go backend/internal/api/share_test.go backend/cmd/server/main.go
git commit -m "feat: manage article share snapshots"
```


## Task 4: Public Snapshot, Share-Gated Assets and Rate Limiting

**Files:**

- Create: `backend/internal/api/share_rate_limit.go`
- Create: `backend/internal/api/share_rate_limit_test.go`
- Modify: `backend/internal/api/share.go`
- Modify: `backend/internal/api/share_test.go`
- Modify: `backend/internal/api/article_images.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write failing public-contract tests**

Add tests for:

```go
func TestPublicShareReturnsSnapshotAndRewritesLocalImages(t *testing.T) {
    f := newPublicShareFixture(t)
    token := f.createActiveShare(t, "Body ![](/api/articles/42/images/0.png)")
    got := f.get(t, "/api/share/"+url.PathEscape(token))
    if got.Code != http.StatusOK { t.Fatalf("status=%d body=%s", got.Code, got.Body.String()) }
    body := got.Body.String()
    if !strings.Contains(body, "/api/share/"+token+"/assets/0.png") { t.Fatalf("local image not rewritten: %s", body) }
    for _, forbidden := range []string{`"article_id"`, `"feed_id"`, `"created_by"`} { if strings.Contains(body, forbidden) { t.Fatalf("leaked %s: %s", forbidden, body) } }
}

func TestPublicShareUsesOneGeneric404ForAllInvalidStates(t *testing.T) {
    f := newPublicShareFixture(t)
    for _, path := range f.invalidSharePaths(t) {
        got := f.get(t, path)
        if got.Code != http.StatusNotFound || got.Body.String() != `{"error":"share unavailable"}` { t.Fatalf("path=%s status=%d body=%q", path, got.Code, got.Body.String()) }
    }
}

func TestPublicShareSnapshotDoesNotChangeAfterArticleUpdate(t *testing.T) {
    f := newPublicShareFixture(t)
    token := f.createActiveShare(t, "Original body")
    f.updateArticle(t, "Changed body")
    got := f.get(t, "/api/share/"+url.PathEscape(token))
    if !strings.Contains(got.Body.String(), "Original body") || strings.Contains(got.Body.String(), "Changed body") { t.Fatalf("snapshot changed: %s", got.Body.String()) }
}

func TestShareAssetChecksShareBeforeServingFile(t *testing.T) {
    f := newPublicShareFixture(t)
    token := f.createShareWithPNG(t, []byte("png-bytes"))
    if got := f.get(t, "/api/share/"+url.PathEscape(token)+"/assets/0.png"); got.Code != http.StatusOK || got.Body.String() != "png-bytes" || got.Header().Get("ETag") == "" { t.Fatalf("active asset status=%d headers=%v", got.Code, got.Header()) }
    f.revokeByToken(t, token)
    if got := f.get(t, "/api/share/"+url.PathEscape(token)+"/assets/0.png"); got.Code != http.StatusNotFound { t.Fatalf("revoked asset status=%d", got.Code) }
}

func TestLegacyShareResolvesByDigestOnlyDuringGrace(t *testing.T) {
    f := newPublicShareFixture(t)
    f.insertLegacyShare(t, sharetoken.LegacyDigest("aB3dE6gH"), f.now.Add(time.Hour))
    if got := f.get(t, "/api/share/aB3dE6gH"); got.Code != http.StatusOK { t.Fatalf("legacy status=%d", got.Code) }
    f.now = f.now.Add(time.Hour)
    if got := f.get(t, "/api/share/aB3dE6gH"); got.Code != http.StatusNotFound { t.Fatalf("expired legacy status=%d", got.Code) }
}
```

Implement the referenced public fixture helpers with a real migrated test database, a deterministic handler clock and `httptest`; the only filesystem substitute is `t.TempDir()` for PDF bytes.

The asset test must write a temporary PNG beneath the test image directory, verify valid bytes plus ETag for an active token, and verify 404 for revoked, expired and tampered tokens.

- [ ] **Step 2: Write failing limiter tests**

Pin a fixed-window limiter with an injected clock:

```go
func TestShareRateLimiterAllowsSixtyThenReturns429(t *testing.T) {
    now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
    calls := 0
    r := gin.New()
    r.GET("/share", NewShareRateLimiter(60, time.Minute, 4096, func() time.Time { return now }), func(c *gin.Context) { calls++; c.Status(204) })
    for i := 0; i < 61; i++ { req := httptest.NewRequest(http.MethodGet, "/share", nil); req.RemoteAddr = "203.0.113.10:1234"; w := httptest.NewRecorder(); r.ServeHTTP(w, req); if i < 60 && w.Code != 204 { t.Fatalf("request %d status=%d", i, w.Code) }; if i == 60 && (w.Code != 429 || w.Header().Get("Retry-After") == "") { t.Fatalf("limited status=%d headers=%v", w.Code, w.Header()) } }
    if calls != 60 { t.Fatalf("downstream calls=%d", calls) }
}

func TestShareRateLimiterSeparatesClientIPsAndResetsWindow(t *testing.T) {
    now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
    limiter := newShareRateLimiterState(1, time.Minute, 16, func() time.Time { return now })
    if !limiter.allow("203.0.113.1") || !limiter.allow("203.0.113.2") || limiter.allow("203.0.113.1") { t.Fatal("per-IP limit mismatch") }
    now = now.Add(time.Minute)
    if !limiter.allow("203.0.113.1") { t.Fatal("window did not reset") }
}

func TestShareRateLimiterBoundsItsIPMap(t *testing.T) {
    limiter := newShareRateLimiterState(1, time.Minute, 2, time.Now)
    for _, ip := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} { limiter.allow(ip) }
    if got := limiter.clientCount(); got != 2 { t.Fatalf("client map size=%d", got) }
}
```

The request after the limit must contain `Retry-After` and must not invoke the downstream handler.

- [ ] **Step 3: Run tests and verify RED**

Run: `cd backend && go test ./internal/api -run 'TestPublicShare|TestShareAsset|TestLegacyShare|TestShareRateLimiter' -count=1`

Expected: FAIL because the public handler, asset route and limiter are not implemented.

- [ ] **Step 4: Implement public resolution and response hardening**

Implement:

```go
func (h *ShareHandler) GetPublic(c *gin.Context)
func (h *ShareHandler) GetAsset(c *gin.Context)
```

`GetPublic` calls `signer.Parse`. For v1 tokens it resolves by public ID; for legacy shapes it resolves `sharetoken.LegacyDigest(token)`. Any parse or unavailable-row condition returns exactly `404 {"error":"share unavailable"}`. Successful responses set:

```text
Cache-Control: no-store
Referrer-Policy: no-referrer
X-Content-Type-Options: nosniff
```

Before serializing, rewrite exact local PDF paths matching `/api/articles/<numeric-id>/images/<numeric-index>.<png|jpg|jpeg>` to `/api/share/<url-escaped-original-token>/assets/<index.ext>`. Do not rewrite external URLs or accept an article ID from the public asset path.

Refactor `ArticleImageHandler.Serve` so both routes call:

```go
func (h *ArticleImageHandler) serve(c *gin.Context, articleID int, idxStr string, cacheControl string)
```

The existing `/api/articles/:id/images/:idx` route passes `public, max-age=31536000, immutable`. `GetAsset` resolves the active share, takes `articleID` only from that row, and passes `c.Param("asset")` plus `no-store` to `serve`; the shared helper must use the supplied cache policy instead of overwriting it. This preserves normal PDF performance while making revocation effective on subsequent share-asset loads.

- [ ] **Step 5: Implement the bounded fixed-window limiter**

Implement a mutex-protected map keyed by `c.ClientIP()` with constructor:

```go
type shareRateLimiter struct {
    mu         sync.Mutex
    clients    map[string]shareWindow
    limit      int
    window     time.Duration
    maxClients int
    now        func() time.Time
}

func newShareRateLimiterState(limit int, window time.Duration, maxClients int, now func() time.Time) *shareRateLimiter
func (l *shareRateLimiter) allow(clientIP string) bool
func (l *shareRateLimiter) clientCount() int
func NewShareRateLimiter(limit int, window time.Duration, maxClients int, now func() time.Time) gin.HandlerFunc
```

Use `60`, `time.Minute`, and `4096` in production. Remove expired entries when a new client arrives and, when still full, evict the entry with the oldest reset time. Return 429 with `Retry-After`. Do not parse `X-Forwarded-For` directly; `c.ClientIP()` must rely on the trusted-proxy configuration already set in `main.go`.

- [ ] **Step 6: Register public routes without the old RLS resolver**

Because the response is a self-contained snapshot and the signature authorizes one row, register both routes directly behind the limiter:

```go
shareLimit := api.NewShareRateLimiter(60, time.Minute, 4096, time.Now)
router.GET("/api/share/:token", shareLimit, shareHandler.GetPublic)
router.GET("/api/share/:token/assets/:asset", shareLimit, shareHandler.GetAsset)
```

Remove the obsolete `ResolveOwner` path. Keep bookmarklet and extension uses of `PublicTokenMiddleware` unchanged.

- [ ] **Step 7: Run public API tests and the full API package**

Run: `cd backend && go test ./internal/api -count=1`

Expected: PASS.

- [ ] **Step 8: Commit the public boundary**

```bash
git add backend/internal/api/share.go backend/internal/api/share_test.go backend/internal/api/share_rate_limit.go backend/internal/api/share_rate_limit_test.go backend/internal/api/article_images.go backend/cmd/server/main.go
git commit -m "feat: serve protected public article snapshots"
```

## Task 5: Share API Client and Management Dialog

**Files:**

- Create: `frontend/src/components/ShareDialog.tsx`
- Create: `frontend/test/ShareDialog.test.tsx`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write failing dialog tests**

Mock `listArticleShares`, `createArticleShare`, and `revokeArticleShare`. Cover:

```tsx
it('creates permanent and custom-expiry links and copies each returned URL', async () => {
  apiMocks.createArticleShare.mockResolvedValue(activeShare('new-id', '/share/signed'))
  renderShareDialog()
  await userEvent.click(await screen.findByRole('button', { name: '创建新链接' }))
  await waitFor(() => expect(navigator.clipboard.writeText).toHaveBeenCalledWith('https://rss.example/share/signed'))
  expect(apiMocks.createArticleShare).toHaveBeenCalledWith(42, null)
})

it('lists active expired revoked and legacy rows with their allowed actions', async () => {
  apiMocks.listArticleShares.mockResolvedValue([activeShare('a'), expiredShare('b'), revokedShare('c'), legacyShare('d')])
  renderShareDialog()
  expect(await screen.findByText('已过期')).toBeInTheDocument()
  expect(screen.getByText('已撤销')).toBeInTheDocument()
  expect(screen.getByText('旧版链接')).toBeInTheDocument()
  expect(screen.getAllByRole('button', { name: '复制链接' })).toHaveLength(1)
})

it('revokes one row without changing the other rows', async () => {
  apiMocks.listArticleShares.mockResolvedValue([activeShare('a'), activeShare('b')])
  apiMocks.revokeArticleShare.mockResolvedValue(revokedShare('a'))
  renderShareDialog()
  await userEvent.click((await screen.findAllByRole('button', { name: '撤销' }))[0])
  expect(await screen.findByText('已撤销')).toBeInTheDocument()
  expect(screen.getByText('b')).toBeInTheDocument()
})

it('shows the 409 not-ready message without closing the dialog', async () => {
  apiMocks.createArticleShare.mockRejectedValue({ response: { status: 409 } })
  renderShareDialog()
  await userEvent.click(await screen.findByRole('button', { name: '创建新链接' }))
  expect(await screen.findByText('文章正文尚未准备完成')).toBeInTheDocument()
  expect(screen.getByRole('dialog')).toBeInTheDocument()
})
```

In the same file, define `apiMocks`, `activeShare`, `expiredShare`, `revokedShare`, `legacyShare`, and `renderShareDialog` as concrete fixtures. Set JSDOM's URL to `https://rss.example/articles/42`, use `userEvent`, and mock `navigator.clipboard.writeText`.

- [ ] **Step 2: Run dialog tests and verify RED**

Run: `cd frontend && npm test -- ShareDialog.test.tsx`

Expected: FAIL because the component and client methods do not exist.

- [ ] **Step 3: Add exact client types and methods**

Replace `ShareInfo` and `shareArticle` with:

```ts
export type ArticleShareStatus = 'active' | 'expired' | 'revoked'

export interface ArticleShareListItem {
  id: string
  url?: string
  created_at: string
  expires_at: string | null
  status: ArticleShareStatus
  legacy: boolean
}

export const listArticleShares = (articleId: number) =>
  api.get<ArticleShareListItem[]>(`/articles/${articleId}/shares`).then(r => r.data)

export const createArticleShare = (articleId: number, expiresAt: string | null) =>
  api.post<ArticleShareListItem>(`/articles/${articleId}/shares`, { expires_at: expiresAt }).then(r => r.data)

export const revokeArticleShare = (articleId: number, shareId: string) =>
  api.delete<ArticleShareListItem>(`/articles/${articleId}/shares/${shareId}`).then(r => r.data)
```

- [ ] **Step 4: Implement `ShareDialog` and replace inline handlers**

Props:

```ts
type ShareDialogProps = {
  articleId: number
  articleTitle: string
  open: boolean
  onClose(): void
  onCopyXiaohongshu(): void
  onExportMarkdown(): void
}
```

The component loads rows only when opened, offers `7d`, `30d`, `custom`, and `permanent`, converts a custom date to ISO, disables submit while pending, copies the absolute URL with `new URL(result.url!, window.location.origin).toString()`, and updates only the affected row after revocation. Each active non-legacy row exposes “复制链接”和“分享到 X”; X opens `https://twitter.com/intent/tweet` with the article title and that row's signed URL. The dialog also preserves the existing Xiaohongshu-copy and Markdown-export actions through the two callbacks.

In `ArticlePage.tsx`, remove `shareToken`, `getOrFetchShareToken`, `handleShareTwitter`, `handleCopyLink`, and the current inline share card. Keep `handleShareXiaohongshu` and `handleExportMarkdown`, pass them into the dialog, and add one “分享” button that opens `ShareDialog`.

- [ ] **Step 5: Add focused styles**

Add `.share-dialog`, `.share-expiry-options`, `.share-list`, `.share-status`, and narrow-screen rules. Reuse existing button, card, muted-text, color and overlay tokens.

- [ ] **Step 6: Run dialog and existing article tests**

Run: `cd frontend && npm test -- ShareDialog.test.tsx ArticlePageCachedRender.test.tsx`

Expected: PASS. Update old test mocks from `shareArticle` to the three new methods only where the `ArticlePage` import surface requires them.

- [ ] **Step 7: Commit management UI**

```bash
git add frontend/src/api/client.ts frontend/src/components/ShareDialog.tsx frontend/src/pages/ArticlePage.tsx frontend/src/index.css frontend/test/ShareDialog.test.tsx frontend/test/ArticlePageCachedRender.test.tsx
git commit -m "feat: manage article share links"
```

## Task 6: Presentation-Only Public Article Reader

**Files:**

- Create: `frontend/src/components/PublicArticleReader.tsx`
- Create: `frontend/test/SharePage.test.tsx`
- Modify: `frontend/src/pages/SharePage.tsx`
- Modify: `frontend/src/components/MarkdownArticle.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write failing public-page tests**

Mock plain Axios, not the authenticated `api` client. Cover:

```tsx
it('renders title source summaries full markdown reading metadata and original link', async () => {
  axiosMock.get.mockResolvedValue({ data: sharedSnapshot({ content: '# Full body' }) })
  renderSharePage('/share/v1_token')
  expect(await screen.findByRole('heading', { name: 'Shared title' })).toBeInTheDocument()
  expect(screen.getByText('Brief summary')).toBeInTheDocument()
  expect(screen.getByText('Full body')).toBeInTheDocument()
  expect(screen.getByRole('link', { name: '阅读原文' })).toHaveAttribute('href', 'https://source.example/post')
})

it('renders public media but replaces private relay media with an origin link', async () => {
  axiosMock.get.mockResolvedValue({ data: sharedSnapshot({ media_url: '/api/media/youtube/private-ticket', media_type: 'youtube' }) })
  renderSharePage('/share/v1_token')
  expect(await screen.findByText('前往原网站播放')).toBeInTheDocument()
  expect(document.querySelector('video,audio,iframe')).toBeNull()
})

it('never calls progress preference event or tag APIs', async () => {
  axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
  renderSharePage('/share/v1_token')
  await screen.findByText('Full body')
  expect(privateAPIMocks.updateProgress).not.toHaveBeenCalled()
  expect(privateAPIMocks.recordReadDuration).not.toHaveBeenCalled()
  expect(privateAPIMocks.getArticleTags).not.toHaveBeenCalled()
})

it('shows unavailable for 404 and offers retry for a network failure', async () => {
  axiosMock.get.mockRejectedValueOnce({ response: { status: 404 } })
  const view = renderSharePage('/share/missing')
  expect(await screen.findByText('分享链接无效或已过期')).toBeInTheDocument()
  view.unmount()
  axiosMock.get.mockRejectedValueOnce(new Error('network'))
  renderSharePage('/share/v1_token')
  expect(await screen.findByRole('button', { name: '重试' })).toBeInTheDocument()
})

it('renders both authentication CTAs with source intent preserved', async () => {
  axiosMock.get.mockResolvedValue({ data: sharedSnapshot() })
  renderSharePage('/share/v1_token')
  expect(await screen.findByRole('link', { name: '使用 RSS Pal' })).toHaveAttribute('href', '/login?intent=use')
  expect(screen.getByRole('link', { name: '订阅原始来源' }).getAttribute('href')).toContain('intent=subscribe')
})

it('blocks unsafe markdown and media protocols', async () => {
  axiosMock.get.mockResolvedValue({ data: sharedSnapshot({ content: '[bad](javascript:alert(1))', media_url: 'file:///etc/passwd', media_type: 'audio' }) })
  renderSharePage('/share/v1_token')
  await screen.findByRole('heading', { name: 'Shared title' })
  expect(document.querySelector('a[href^="javascript:"],audio[src^="file:"]')).toBeNull()
})
```

Define `sharedSnapshot`, `renderSharePage`, `axiosMock`, and `privateAPIMocks` in this test file; mock `MarkdownArticle` only to expose its `source` and `readOnly` props, while leaving `SummaryMarkdown` rendering real.

Assert outbound anchors have `rel="noopener noreferrer"`. Assert the page installs `<meta name="referrer" content="no-referrer">` because the static frontend host cannot set a route-specific document header.

- [ ] **Step 2: Run public-page tests and verify RED**

Run: `cd frontend && npm test -- SharePage.test.tsx`

Expected: FAIL because the current page renders summaries only.

- [ ] **Step 3: Define the public response and renderer props**

Use an explicit frontend type matching `ArticleShareSnapshot`:

```ts
export interface SharedArticleSnapshot {
  title: string
  url: string
  feed_title?: string
  published_at?: string | null
  word_count?: number
  reading_minutes?: number
  summary_brief?: string
  summary_detailed?: string
  content: string
  media_url?: string
  media_type?: string
  media_duration_seconds?: number
  image_dimensions?: Record<string, [number, number]>
  snapshotted_at: string
}
```

- [ ] **Step 4: Implement a presentation-only reader**

`PublicArticleReader` renders metadata, `SummaryMarkdown`, `MarkdownArticle`, public `VideoEmbed` or native audio where supported, the original link and footer CTAs. It must not import `ReaderActionContext`, progress hooks, player context, preference APIs, tag APIs or event APIs.

If `MarkdownArticle` currently always installs `ReaderInteractionSurface`, add a `readOnly?: boolean` prop defaulting to `false`; when true, render the Markdown subtree without selection or context-menu actions. Preserve current behavior for authenticated callers.

Treat as non-public any `media_url` whose path begins `/api/media/youtube/` or whose scheme is not `http:` or `https:`. Render a source-site fallback instead of attempting playback. Keep React Markdown's safe-protocol behavior and add an explicit URL-scheme helper for media sources; never enable raw HTML rendering.

- [ ] **Step 5: Replace `SharePage`**

Continue using unauthenticated `axios.get('/api/share/'+encodeURIComponent(token))`. Distinguish an HTTP 404 unavailable response from a network or 5xx retryable failure. Do not route a public 404 through the authenticated Axios interceptor.

Install the no-referrer meta policy on mount and restore the previous meta content on unmount. Set `document.title` to `<snapshot title> - RSS Pal` after load.

- [ ] **Step 6: Run public and Markdown regressions**

Run: `cd frontend && npm test -- SharePage.test.tsx MarkdownArticleAnchors.test.tsx ReaderInteractionSurface.test.tsx`

Expected: PASS and authenticated Markdown interaction behavior remains unchanged.

- [ ] **Step 7: Commit public reader**

```bash
git add frontend/src/components/PublicArticleReader.tsx frontend/src/components/MarkdownArticle.tsx frontend/src/pages/SharePage.tsx frontend/src/index.css frontend/test/SharePage.test.tsx frontend/test/MarkdownArticleAnchors.test.tsx frontend/test/ReaderInteractionSurface.test.tsx
git commit -m "feat: render full shared articles for visitors"
```

## Task 7: Login, Registration and Subscription Intent

**Files:**

- Create: `frontend/src/utils/authIntent.ts`
- Create: `frontend/test/AuthIntentRoutes.test.tsx`
- Modify: `frontend/src/pages/LoginPage.tsx`
- Modify: `frontend/src/pages/RegisterPage.tsx`
- Modify: `frontend/src/pages/FeedListPage.tsx`
- Modify: `frontend/src/components/PublicArticleReader.tsx`

- [ ] **Step 1: Write failing auth-intent tests**

Cover these exact behaviors:

```tsx
it('Use RSS Pal logs in to /articles', async () => {
  renderAuthRoute('/login?intent=use')
  await submitValidLogin()
  expect(screen.getByTestId('location')).toHaveTextContent('/articles')
})

it('subscribe intent survives login-register-login links', async () => {
  renderAuthRoute('/login?intent=subscribe&source=https%3A%2F%2Fsource.example%2Fpost')
  const register = await screen.findByRole('link', { name: '使用邀请码注册' })
  expect(register.getAttribute('href')).toContain('intent=subscribe')
  expect(register.getAttribute('href')).toContain('source=')
})

it('successful subscribe login lands on /feeds with a validated source URL', async () => {
  renderAuthRoute('/login?intent=subscribe&source=https%3A%2F%2Fsource.example%2Fpost')
  await submitValidLogin()
  expect(screen.getByTestId('location').textContent).toBe('/feeds?add=1&source=https%3A%2F%2Fsource.example%2Fpost')
})

it('rejects javascript non-http and oversized source values', () => {
  expect(parseAuthIntent('?intent=subscribe&source=javascript%3Aalert(1)')).toEqual({ kind: 'use', returnTo: '/articles' })
  expect(parseAuthIntent('?intent=subscribe&source=' + 'x'.repeat(2049))).toEqual({ kind: 'use', returnTo: '/articles' })
})

it('prefills and previews the source page once without auto-subscribing', async () => {
  renderFeedRoute('/feeds?add=1&source=https%3A%2F%2Fsource.example%2Fpost')
  expect(await screen.findByDisplayValue('https://source.example/post')).toBeInTheDocument()
  expect(apiMocks.previewFeed).toHaveBeenCalledTimes(1)
  expect(apiMocks.addFeed).not.toHaveBeenCalled()
})
```

Define `renderAuthRoute`, `submitValidLogin`, and `renderFeedRoute` with `MemoryRouter`, a location probe and mocked auth/feed API calls. Assert registration still renders the invitation-code field.

- [ ] **Step 2: Run auth-intent tests and verify RED**

Run: `cd frontend && npm test -- AuthIntentRoutes.test.tsx`

Expected: FAIL because intent parsing and post-auth navigation are absent.

- [ ] **Step 3: Implement the pure intent helper**

Define:

```ts
export type AuthIntent =
  | { kind: 'use'; returnTo: '/articles' }
  | { kind: 'subscribe'; returnTo: '/feeds'; source: string }

export function parseAuthIntent(search: string): AuthIntent
export function authSearch(intent: AuthIntent): string
export function postAuthURL(intent: AuthIntent): string
```

Only accept absolute `http:` or `https:` source URLs up to 2048 characters. `postAuthURL` returns `/articles` for use and `/feeds?add=1&source=<encoded>` for subscribe. There is no caller-controlled general `next` value, eliminating open redirects by construction.

- [ ] **Step 4: Preserve intent through login and registration**

Both pages call `parseAuthIntent(location.search)`, use `postAuthURL` after successful authentication, and generate the other auth page's `Link` with normalized `authSearch(intent)`. Keep all existing credential and invitation-code behavior unchanged.

- [ ] **Step 5: Prefill but do not auto-subscribe in `FeedListPage`**

Read `add=1` and `source` once on mount. Validate with the same helper, set `newUrl`, call existing `doPreview(source)`, then remove `add` and `source` from the browser URL with `navigate('/feeds', {replace:true})`. Never call `addFeed` until the user confirms the preview.

- [ ] **Step 6: Wire CTA URLs**

“使用 RSS Pal” links to `/login?intent=use`. “订阅原始来源” links to `/login?intent=subscribe&source=<snapshot.url>`. Both stay same-origin and retain the share page in normal browser history so failed authentication can use Back.

- [ ] **Step 7: Run auth, feed and public-page tests**

Run: `cd frontend && npm test -- AuthIntentRoutes.test.tsx SharePage.test.tsx`

Expected: PASS.

- [ ] **Step 8: Commit auth intent**

```bash
git add frontend/src/utils/authIntent.ts frontend/src/pages/LoginPage.tsx frontend/src/pages/RegisterPage.tsx frontend/src/pages/FeedListPage.tsx frontend/src/components/PublicArticleReader.tsx frontend/test/AuthIntentRoutes.test.tsx frontend/test/SharePage.test.tsx
git commit -m "feat: carry share actions through authentication"
```

## Task 8: Documentation, Legacy Contract and Security Regression Sweep

**Files:**

- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `backend/internal/api/share_test.go`
- Modify: `backend/internal/repository/share_test.go`
- Modify: `frontend/test/SharePage.test.tsx`

- [ ] **Step 1: Add contract assertions before documentation**

Add table-driven assertions that public JSON contains none of these names at any depth:

```text
created_by user_id feed_id editor_note processing_error is_read manual_tags
```

Add a repository/API test with distinct article and feed URLs and confirm that only the intentionally public article URL from the snapshot is carried through the CTA; the feed repository URL, including any feed query credentials, is never returned.

- [ ] **Step 2: Run the security tests**

Run: `cd backend && go test ./internal/api ./internal/repository -run 'Share' -count=1 && cd ../frontend && npm test -- SharePage.test.tsx AuthIntentRoutes.test.tsx`

Expected: PASS.

- [ ] **Step 3: Update repository documentation**

Document the new API routes, immutable snapshot behavior, multiple-link lifecycle, `SHARE_SECRET`, 30-day old-link grace period, and the explicit fact that remote media bytes are not archived. In `CLAUDE.md`, replace the old `share_tokens` public-resolver example with `article_shares` guidance and retain `PublicTokenMiddleware` guidance for bookmarklet and extension routes.

- [ ] **Step 4: Verify no old runtime references remain**

Run:

```bash
rg -n 'share_tokens|GetOrCreate\(|generateShareToken|POST /api/articles/:id/share|shareArticle\(' backend frontend/src README.md CLAUDE.md
```

Expected: only migration-039 legacy-copy SQL and historical migration comments may match; no compiled Go or TypeScript caller and no active route may match.

- [ ] **Step 5: Commit documentation and regression guards**

```bash
git add README.md CLAUDE.md backend/internal/api/share_test.go backend/internal/repository/share_test.go frontend/test/SharePage.test.tsx
git commit -m "docs: document secure article sharing"
```

## Task 9: Full Verification and Delivery Readiness

**Files:**

- Verify only; fix failures in the owning task's files and amend that task's commit.

- [ ] **Step 1: Run complete backend verification**

Run:

```bash
cd backend
go test ./... -count=1
go test -race ./internal/sharetoken ./internal/api ./internal/repository -count=1
go vet ./...
```

Expected: all tests PASS, the race detector reports no races, and `go vet` exits 0. Database-backed tests must actually execute against PostgreSQL; an environment-wide skip is not sufficient final verification.

- [ ] **Step 2: Run complete frontend verification**

Use the repository's supported Node 22 runtime, then run:

```bash
cd frontend
npm test
npm run test:legacy
npm run build
```

Expected: all Vitest and legacy Node tests PASS; TypeScript and Vite production build exit 0.

- [ ] **Step 3: Verify migration on fresh and legacy fixtures**

Run the repository migration harness for a fresh schema, then create a migration-038 schema with one real `share_tokens` row and apply migration 039 twice.

Expected after the first application: one `article_shares` row, no `share_tokens` table, a 64-character legacy digest, snapshot content present, and expiry approximately 30 days after migration. Expected after the second application: no duplicate row and no error.

- [ ] **Step 4: Perform an incognito end-to-end smoke test locally**

Create two shares for one ready article: permanent and near-future expiry. Open each URL without localStorage credentials and verify full content, summaries, remote image behavior, local PDF image behavior, public media fallback, both CTAs, independent revocation, and unchanged snapshot after editing the source article.

Expected: the permanent share stays available; revoking the other makes its JSON and asset paths return the same generic 404; no request to progress, preference, event, tag or private article endpoints appears in the network log.

- [ ] **Step 5: Check scope and commits**

Run:

```bash
git diff --check codex/external-article-sharing...HEAD
git status --short
git log --oneline codex/external-article-sharing..HEAD
```

Expected: no whitespace errors; only planned files are tracked as changed; the user's pre-existing backup and `rss-pal-course/` files remain untouched and untracked; commits correspond to Tasks 1 through 8.

- [ ] **Step 6: Request code review before integration**

Use the repository's code-review workflow against `codex/external-article-sharing...HEAD`. Resolve all correctness, authorization, token, migration, caching and private-data findings, then rerun Steps 1 through 5. Do not merge, push or deploy based only on focused test success.

## Delivery Boundary

After implementation and code review are verified, follow `CLAUDE.md`: merge the implementation branch into `master`, push `master`, wait for `.github/workflows/deploy-tencent.yml`, and verify the exact deployed revision, `status-migrate` exit code, running Tencent containers, direct and public API health, public frontend bundle, one real new share URL, one revoked URL, and one share-gated local asset. Publishing and deployment begin only when the user asks to implement or otherwise authorizes that delivery phase; this plan creation does not itself authorize it.
