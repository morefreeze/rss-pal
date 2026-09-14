# RSS Pal Short Share Domain Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every newly created article share return a 12-character Base62 URL on `https://r.morefreeze.top`, render the snapshot without redirecting, preserve every historical link, and keep bearer values out of access logs.

**Architecture:** Add a nullable, unique short code to the existing immutable `article_shares` row and resolve it through dedicated public API routes. The React bundle selects a restricted route table by hostname, while the container and Tencent host Nginx layers expose only the short reader surface for the short domain. Existing `public_id + HMAC` and legacy token routes remain intact for historical rows.

**Tech Stack:** Go 1.25, Gin, PostgreSQL 15, React 19, TypeScript, React Router, Vitest, Nginx, Docker Compose, GitHub Actions, Certbot.

---

## Execution Routing

Use `superpowers:subagent-driven-development`. The migration/repository, API/security logging, frontend reader, and Nginx/deployment work are independently reviewable, while their contracts are fixed below and can be integrated in dependency order.

Execute in the existing isolated worktree:

```bash
cd /Users/bytedance/mygit/rss-pal/.worktrees/codex/short-share-domain
```

Do not stage or modify the untracked backup files or `rss-pal-course/` in the main checkout.

## Locked File Map

- Create `backend/migrations/041_article_share_short_codes.sql`: nullable Base62 short-code column, format constraint, and partial unique index.
- Create `backend/internal/repository/share_short_code_migration_test.go`: migration upgrade, reapply, format, case, and history-preservation tests.
- Modify `backend/internal/model/article_share.go`: expose nullable `ShortCode` only to internal Go code.
- Create `backend/internal/sharetoken/shortcode.go`: cryptographic Base62 generation, rejection sampling, and format validation.
- Create `backend/internal/sharetoken/shortcode_test.go`: deterministic byte-stream and random-source failure tests.
- Modify `backend/internal/config/config.go` and `backend/internal/config/share_test.go`: load and strictly validate `SHORT_SHARE_ORIGIN`.
- Modify `backend/internal/repository/share.go` and `backend/internal/repository/share_test.go`: atomic conflict-safe insert and short-code lookup.
- Modify `backend/internal/api/share.go`, `backend/internal/api/share_test.go`, and `backend/internal/api/share_rewrite_test.go`: retry creation, absolute short management URLs, short snapshot and asset endpoints.
- Create `backend/internal/api/access_log.go` and `backend/internal/api/access_log_test.go`: route-template access logging and redacted panic recovery.
- Modify `backend/cmd/server/main.go`: validate the short origin, install safe middleware, and register short routes with existing rate limiters.
- Modify `frontend/src/App.tsx` and create `frontend/test/ShortShareRoutes.test.tsx`: hostname-isolated route tables that never initialize private-session code on the short domain.
- Modify `frontend/src/pages/SharePage.tsx` and `frontend/test/SharePage.test.tsx`: one loader for long and short identifiers.
- Modify `frontend/src/components/PublicArticleReader.tsx`, `frontend/src/components/MarkdownArticle.tsx`, and their tests: absolute main-site CTAs, canonical short share URL, and strict short asset allow-list.
- Modify `frontend/src/utils/xShare.ts` and `frontend/test/XShare.test.ts`: enforce the confirmed 140 weighted-character budget while preserving title, summary, and URL priority.
- Modify `frontend/test/ShareDialog.test.tsx` to prove absolute short URLs are copied and passed to the existing owner-side X intent unchanged; production component behavior already accepts absolute URLs.
- Modify `frontend/nginx.conf` and create `frontend/test/nginxShortShareHost.test.cjs`: a separate short-domain virtual host with a narrow allow-list and no raw access log.
- Create `deploy/nginx/r-morefreeze-bootstrap.conf` and `deploy/nginx/rss-pal-tencent.conf`: reviewable Tencent host bootstrap and final TLS virtual hosts.
- Create `deploy/tencent/rss-pal-deploy-from-actions`: reviewable Actions wrapper that explicitly selects direct deployment networking.
- Create `scripts/tests/tencent_short_domain_nginx_test.sh`: static assertions for host routing, TLS paths, proxy target, and access-log policy.
- Modify `scripts/auto_deploy.sh` and `scripts/tests/auto_deploy_service_selection_test.sh`; create `scripts/tests/auto_deploy_direct_network_test.sh`: install the frontend before a changed backend and keep Actions deployment off the proxy without changing worker scraping egress.
- Modify `.env.example`, `docker-compose.yml`, `.github/workflows/deploy-tencent.yml`, and `README.md`: configuration, migration order, direct-network production checks, and operator documentation.

### Task 1: Add the backward-compatible database migration

**Files:**
- Create: `backend/migrations/041_article_share_short_codes.sql`
- Create: `backend/internal/repository/share_short_code_migration_test.go`
- Modify: `backend/internal/model/article_share.go`

- [ ] **Step 1: Write the failing migration tests**

Create `share_short_code_migration_test.go` with one upgrade-path test that starts through migration 040, inserts a historical share, applies migration 041 twice, and verifies these exact properties:

```go
func TestMigration041AddsNullableCaseSensitiveShortCodes(t *testing.T) {
	db, cleanup := testdb.NewThroughMigration(t, "040_explore_provider_materialized_at.sql")
	defer cleanup()

	userID, feedID, articleID := seedShortCodeMigrationArticle(t, db)
	const publicID = "11111111111111111111111111111111"
	if _, err := db.Exec(`
		INSERT INTO article_shares(public_id, article_id, created_by, snapshot)
		VALUES ($1, $2, $3, '{"title":"old","url":"https://example.test","content":"body","snapshotted_at":"2026-09-13T00:00:00Z"}')`,
		publicID, articleID, userID); err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := testdb.ExecuteMigrationFile(db, "041_article_share_short_codes.sql"); err != nil {
			t.Fatalf("migration attempt %d: %v", attempt+1, err)
		}
	}

	var historical sql.NullString
	if err := db.QueryRow(`SELECT short_code FROM article_shares WHERE public_id=$1`, publicID).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if historical.Valid {
		t.Fatalf("historical short_code=%q, want NULL", historical.String)
	}

	insert := func(publicID, shortCode string) error {
		_, err := db.Exec(`INSERT INTO article_shares(public_id,article_id,created_by,snapshot,short_code)
			VALUES($1,$2,$3,'{"title":"new","url":"https://example.test","content":"body","snapshotted_at":"2026-09-13T00:00:00Z"}',$4)`,
			publicID, articleID, userID, shortCode)
		return err
	}
	if err := insert("22222222222222222222222222222222", "Aa0000000000"); err != nil { t.Fatal(err) }
	if err := insert("33333333333333333333333333333333", "aa0000000000"); err != nil { t.Fatal(err) }
	if err := insert("44444444444444444444444444444444", "Aa0000000000"); err == nil {
		t.Fatal("duplicate short code was accepted")
	}
	for _, invalid := range []string{"short", "0000000000000", "00000000000_", "00000000000-"} {
		if err := insert(randomPublicIDForMigrationTest(invalid), invalid); err == nil {
			t.Errorf("invalid short code %q was accepted", invalid)
		}
	}

	_ = feedID
}
```

Define the helpers in the same file so the test is self-contained:

```go
func seedShortCodeMigrationArticle(t *testing.T, db *sql.DB) (userID, feedID, articleID int) {
	t.Helper()
	if err := db.QueryRow(`INSERT INTO users(username,password_hash) VALUES('short-migration','x') RETURNING id`).Scan(&userID); err != nil { t.Fatal(err) }
	if err := db.QueryRow(`INSERT INTO feeds(url,title,owner_id) VALUES('https://short.example/feed','Short',$1) RETURNING id`, userID).Scan(&feedID); err != nil { t.Fatal(err) }
	if err := db.QueryRow(`INSERT INTO articles(feed_id,title,url,content) VALUES($1,'Short','https://short.example/post','body') RETURNING id`, feedID).Scan(&articleID); err != nil { t.Fatal(err) }
	return userID, feedID, articleID
}

func randomPublicIDForMigrationTest(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(digest[:16])
}
```

Add `crypto/sha256` and `encoding/hex` to the test imports.

- [ ] **Step 2: Run the focused test and verify it fails**

Run:

```bash
cd backend
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/repository -run TestMigration041 -count=1
```

Expected: FAIL because `041_article_share_short_codes.sql` does not exist.

- [ ] **Step 3: Add the idempotent migration and internal model field**

Create the migration with this exact DDL:

```sql
ALTER TABLE article_shares
    ADD COLUMN IF NOT EXISTS short_code VARCHAR(12) COLLATE "C";

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conrelid = 'article_shares'::regclass
           AND conname = 'article_shares_short_code_format'
    ) THEN
        ALTER TABLE article_shares
            ADD CONSTRAINT article_shares_short_code_format
            CHECK (short_code IS NULL OR short_code ~ '^[0-9A-Za-z]{12}$');
    END IF;
END
$$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_article_shares_short_code
    ON article_shares (short_code)
    WHERE short_code IS NOT NULL;
```

Add the nullable field beside `PublicID`:

```go
type ArticleShare struct {
	PublicID          string
	ShortCode         *string
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

- [ ] **Step 4: Run migration and model verification**

Run the focused test, then the whole repository package:

```bash
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/repository -run TestMigration041 -count=1
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/repository -count=1
```

Expected: both PASS; historical rows remain `NULL`, `Aa0000000000` and `aa0000000000` coexist, and duplicates/invalid formats fail.

- [ ] **Step 5: Commit the migration boundary**

```bash
git add backend/migrations/041_article_share_short_codes.sql backend/internal/repository/share_short_code_migration_test.go backend/internal/model/article_share.go
git commit -m "feat: add article share short codes"
```

### Task 2: Generate secure Base62 codes and validate the configured origin

**Files:**
- Create: `backend/internal/sharetoken/shortcode.go`
- Create: `backend/internal/sharetoken/shortcode_test.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/share_test.go`

- [ ] **Step 1: Write deterministic short-code tests**

Cover rejection sampling, output format, and reader errors:

```go
func TestNewShortCodeRejectsBiasedBytes(t *testing.T) {
	raw := append([]byte{248, 249, 250, 251, 252, 253, 254, 255},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}...)
	got, err := newShortCode(bytes.NewReader(raw))
	if err != nil { t.Fatal(err) }
	if got != "0123456789AB" {
		t.Fatalf("newShortCode()=%q, want 0123456789AB", got)
	}
}

func TestNewShortCodePropagatesRandomSourceFailure(t *testing.T) {
	_, err := newShortCode(io.MultiReader(strings.NewReader("abc"), errorReader{}))
	if err == nil || !strings.Contains(err.Error(), "generate short code") {
		t.Fatalf("err=%v", err)
	}
}

type errorReader struct{}
func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }

func TestNewShortCodeShape(t *testing.T) {
	got, err := NewShortCode()
	if err != nil || !IsValidShortCode(got) || len(got) != ShortCodeLength {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
```

- [ ] **Step 2: Write fixed production-origin tests**

Extend `share_test.go` so only the exact production origin is accepted:

```go
func TestValidateShortShareOrigin(t *testing.T) {
	const valid = "https://r.morefreeze.top"
	if got, err := ValidateShortShareOrigin(valid); err != nil || got != valid {
		t.Errorf("ValidateShortShareOrigin(%q)=%q,%v", valid, got, err)
	}
	for _, invalid := range []string{"", "http://r.morefreeze.top", "https://short.example.test", "https://r.morefreeze.top:443", "https://R.morefreeze.top", "https://r.morefreeze.top.", "https://r.morefreeze.top/", "https://r.morefreeze.top/a", "https://u:p@r.morefreeze.top", "https://r.morefreeze.top?q=1", "https://r.morefreeze.top#x"} {
		if _, err := ValidateShortShareOrigin(invalid); err == nil {
			t.Errorf("ValidateShortShareOrigin(%q) succeeded", invalid)
		}
	}
}
```

- [ ] **Step 3: Run tests and verify both new contracts fail**

```bash
cd backend
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/sharetoken ./internal/config -run 'ShortCode|ShortShareOrigin' -count=1
```

Expected: FAIL because the generator and validator are undefined.

- [ ] **Step 4: Implement rejection sampling**

Create `shortcode.go` with these public contracts and an unexported injectable reader:

```go
package sharetoken

import (
	"crypto/rand"
	"fmt"
	"io"
)

const ShortCodeLength = 12

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
const unbiasedByteLimit = 256 - (256 % len(base62Alphabet))

func NewShortCode() (string, error) { return newShortCode(rand.Reader) }

func newShortCode(reader io.Reader) (string, error) {
	code := make([]byte, 0, ShortCodeLength)
	var sample [1]byte
	for len(code) < ShortCodeLength {
		if _, err := io.ReadFull(reader, sample[:]); err != nil {
			return "", fmt.Errorf("generate short code: %w", err)
		}
		if int(sample[0]) >= unbiasedByteLimit { continue }
		code = append(code, base62Alphabet[int(sample[0])%len(base62Alphabet)])
	}
	return string(code), nil
}

func IsValidShortCode(value string) bool {
	if len(value) != ShortCodeLength { return false }
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')) { return false }
	}
	return true
}
```

- [ ] **Step 5: Load and validate `SHORT_SHARE_ORIGIN`**

Extend `ShareConfig` with `ShortOrigin string`, load `SHORT_SHARE_ORIGIN` without a default, and add:

```go
func ValidateShortShareOrigin(raw string) (string, error) {
	if raw != shortShareOrigin {
		return "", errors.New("SHORT_SHARE_ORIGIN must be exactly https://r.morefreeze.top")
	}
	return shortShareOrigin, nil
}
```

Define `const shortShareOrigin = "https://r.morefreeze.top"` and add the necessary `errors` import. Extend `TestLoadShareSecret` with:

```go
t.Setenv("SHORT_SHARE_ORIGIN", "https://r.morefreeze.top")
cfg := Load()
if cfg.Share.Secret != "share-secret-for-test" { t.Fatalf("secret=%q", cfg.Share.Secret) }
if cfg.Share.ShortOrigin != "https://r.morefreeze.top" { t.Fatalf("origin=%q", cfg.Share.ShortOrigin) }
```

- [ ] **Step 6: Verify generator and configuration packages**

```bash
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/sharetoken ./internal/config -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the pure security primitives**

```bash
git add backend/internal/sharetoken/shortcode.go backend/internal/sharetoken/shortcode_test.go backend/internal/config/config.go backend/internal/config/share_test.go
git commit -m "feat: generate secure share short codes"
```

### Task 3: Persist and resolve short codes atomically

**Files:**
- Modify: `backend/internal/repository/share.go`
- Modify: `backend/internal/repository/share_test.go`

- [ ] **Step 1: Change repository tests to state the new insert contract**

Keep the existing `Create` method and its callers compiling for this intermediate commit. Add `CreateWithShortCode` and test it with a snapshot built once:

```go
snapshot := SnapshotFromArticle(f.article, now)
first, err := f.repo.CreateWithShortCode(f.article.ID, f.userA,
	"11111111111111111111111111111111", "Aa0000000000", snapshot, nil, now)
if err != nil || first == nil || first.ShortCode == nil || *first.ShortCode != "Aa0000000000" {
	t.Fatalf("first=%+v err=%v", first, err)
}

conflict, err := f.repo.CreateWithShortCode(f.article.ID, f.userA,
	"22222222222222222222222222222222", "Aa0000000000", snapshot, nil, now)
if err != nil || conflict != nil {
	t.Fatalf("conflict=%+v err=%v, want nil,nil", conflict, err)
}

resolved, err := f.repo.GetActiveByShortCode("Aa0000000000", now)
if err != nil || resolved == nil || resolved.PublicID != first.PublicID {
	t.Fatalf("resolved=%+v err=%v", resolved, err)
}
```

Also assert short lookup returns nil at the exact expiry boundary and after revocation. Historical rows with `short_code IS NULL` must still scan successfully in list/public-ID/legacy paths.

- [ ] **Step 2: Run repository share tests and verify signature failures**

```bash
cd backend
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/repository -run 'TestShareRepository|TestMigration041' -count=1
```

Expected: build FAIL until the repository signature and scan order are updated.

- [ ] **Step 3: Implement conflict-safe insert and short lookup**

Use this additive signature:

```go
func (r *ShareRepository) CreateWithShortCode(
	articleID, createdBy int,
	publicID, shortCode string,
	snapshot model.ArticleShareSnapshot,
	expiresAt *time.Time,
	now time.Time,
) (*model.ArticleShare, error)
```

Marshal `snapshot`, insert `short_code`, and use `ON CONFLICT DO NOTHING RETURNING`. Wrap the scanner with `nilOnNoRows` so either the public-ID or short-code unique collision returns `(nil, nil)` without aborting the request transaction. Update the existing `Create`, list, revoke, public-ID, and legacy queries to select/return a `NULL` or real `short_code` in the common scan order; the old `Create` continues inserting a historical-compatible `NULL` code and is not used by the production handler after Task 4.

Every SELECT/RETURNING list must use this exact field order:

```sql
public_id, short_code, article_id, created_by, snapshot_version, snapshot,
expires_at, revoked_at, legacy_token_digest, created_at
```

Scan `short_code` through `sql.NullString` and assign `share.ShortCode` only when valid. Add:

```go
func (r *ShareRepository) GetActiveByShortCode(shortCode string, now time.Time) (*model.ArticleShare, error) {
	return nilOnNoRows(scanArticleShare(r.db.QueryRow(`
		SELECT public_id, short_code, article_id, created_by, snapshot_version, snapshot,
		       expires_at, revoked_at, legacy_token_digest, created_at
		  FROM article_shares
		 WHERE short_code = $1
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > $2)`, shortCode, now)))
}
```

- [ ] **Step 4: Verify repository behavior and race-free package tests**

```bash
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/repository -run 'TestShareRepository|TestMigration041' -count=1
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test -race ./internal/repository -run 'TestShareRepository' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the repository contract**

```bash
git add backend/internal/repository/share.go backend/internal/repository/share_test.go
git commit -m "feat: persist article share short codes"
```

### Task 4: Create, return, and serve short shares

**Files:**
- Modify: `backend/internal/api/share.go`
- Modify: `backend/internal/api/share_test.go`
- Modify: `backend/internal/api/share_rewrite_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Extend the API fixture with deterministic generators and routes**

Introduce handler options so production uses secure defaults and tests can force collisions:

```go
type ShareHandlerOptions struct {
	Now          func() time.Time
	ShortOrigin  string
	NewPublicID  func() (string, error)
	NewShortCode func() (string, error)
}
```

Update `NewShareHandler` to accept these options, substituting `time.Now`, `sharetoken.NewPublicID`, and `sharetoken.NewShortCode` only when the corresponding function is nil. In `newShareAPIFixture`, pass `ShortOrigin: "https://short.example.test"`, register `/api/s/:short_code` and its asset route, and make `createShare` assert:

```go
if !strings.HasPrefix(response.URL, "https://short.example.test/") || len(strings.TrimPrefix(response.URL, "https://short.example.test/")) != 12 {
	t.Fatalf("short URL=%q", response.URL)
}
```

- [ ] **Step 2: Add failing endpoint and retry tests**

Add the following tests using the existing `shareAPIFixture`, `assertShareUnavailable`, and `assertPublicSecurityHeaders` helpers. First add this URL helper:

```go
func shortCodeFromURL(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "short.example.test" {
		t.Fatalf("short URL=%q err=%v", raw, err)
	}
	code := strings.TrimPrefix(parsed.Path, "/")
	if !sharetoken.IsValidShortCode(code) { t.Fatalf("short code=%q", code) }
	return code
}
```

Implement the endpoint contract with concrete assertions:

```go
func TestShortShareReturnsSnapshotAndAssetsWithoutRedirect(t *testing.T) {
	f := newShareAPIFixture(t)
	local := fmt.Sprintf("/api/articles/%d/images/0.png", f.article)
	f.setArticleState(t, "ready", "![image]("+local+")")
	created := f.createShare(t, f.userA, `{"expires_at":null}`)
	code := shortCodeFromURL(t, created.URL)

	dir := filepath.Join(f.imageDir, "article_images", strconv.Itoa(f.article))
	if err := os.MkdirAll(dir, 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dir, "0.png"), []byte("short-png"), 0o644); err != nil { t.Fatal(err) }

	page := f.publicRequest(http.MethodGet, "/api/s/"+code)
	if page.Code != http.StatusOK || page.Header().Get("Location") != "" || !strings.Contains(page.Body.String(), "/api/s/"+code+"/assets/0.png") {
		t.Fatalf("status=%d headers=%v body=%s", page.Code, page.Header(), page.Body.String())
	}
	assertPublicSecurityHeaders(t, page)
	asset := f.publicRequest(http.MethodGet, "/api/s/"+code+"/assets/0.png")
	if asset.Code != http.StatusOK || asset.Body.String() != "short-png" { t.Fatalf("asset=%d %q", asset.Code, asset.Body.String()) }
}

func TestShortShareRejectsInvalidShapeBeforeDatabase(t *testing.T) {
	f := newShareAPIFixture(t)
	if _, err := f.privDB.Exec(`DROP TABLE article_shares`); err != nil { t.Fatal(err) }
	for _, code := range []string{"short", "Aa000000000_", "Aa00000000000"} {
		assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/s/"+code))
	}
}

func TestShortShareDatabaseFailureIsInternalError(t *testing.T) {
	f := newShareAPIFixture(t)
	created := f.createShare(t, f.userA, `{"expires_at":null}`)
	code := shortCodeFromURL(t, created.URL)
	if _, err := f.privDB.Exec(`DROP TABLE article_shares`); err != nil { t.Fatal(err) }
	w := f.publicRequest(http.MethodGet, "/api/s/"+code)
	if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"internal server error"}` {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
}
```

Implement lifecycle uniformity as:

```go
func TestShortShareUnavailableStatesAreUniform(t *testing.T) {
	f := newShareAPIFixture(t)
	active := f.createShare(t, f.userA, `{"expires_at":null}`)
	expiring := f.createShare(t, f.userA, `{"expires_at":"2026-09-08T12:00:01Z"}`)
	revoked := f.createShare(t, f.userA, `{"expires_at":null}`)
	if got := f.request(t, http.MethodDelete, f.articlePath("/shares/"+revoked.ID), f.userA, ""); got.Code != http.StatusOK {
		t.Fatalf("revoke=%d body=%s", got.Code, got.Body.String())
	}

	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/s/MissingCode1"))
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/s/"+shortCodeFromURL(t, revoked.URL)))
	f.now = shareAPINow.Add(2 * time.Second)
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/s/"+shortCodeFromURL(t, expiring.URL)))
	if _, err := f.privDB.Exec(`DELETE FROM articles WHERE id=$1`, f.article); err != nil { t.Fatal(err) }
	assertShareUnavailable(t, f.publicRequest(http.MethodGet, "/api/s/"+shortCodeFromURL(t, active.URL)))
}
```

Add `newShareAPIFixtureWithOptions(t, options)` as the implementation behind `newShareAPIFixture`; it must always override `options.Now` with the fixture clock but retain supplied generators. Implement one-collision retry as:

```go
func TestCreateShareRetriesUniqueCollisionAsOneSnapshot(t *testing.T) {
	publicIDs := []string{strings.Repeat("1", 32), strings.Repeat("2", 32)}
	shortCodes := []string{"Collision001", "UniqueCode01"}
	publicCalls, shortCalls := 0, 0
	f := newShareAPIFixtureWithOptions(t, api.ShareHandlerOptions{
		NewPublicID: func() (string, error) { value := publicIDs[publicCalls]; publicCalls++; return value, nil },
		NewShortCode: func() (string, error) { value := shortCodes[shortCalls]; shortCalls++; return value, nil },
	})
	if _, err := f.privDB.Exec(`INSERT INTO article_shares(public_id,short_code,article_id,created_by,snapshot)
		VALUES($1,'Collision001',$2,$3,'{"title":"collision","url":"https://example.test","content":"body","snapshotted_at":"2026-09-01T00:00:00Z"}')`,
		strings.Repeat("f", 32), f.article, f.userA); err != nil { t.Fatal(err) }

	w := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, `{"expires_at":null}`)
	if w.Code != http.StatusCreated || publicCalls != 2 || shortCalls != 2 {
		t.Fatalf("status=%d publicCalls=%d shortCalls=%d body=%s", w.Code, publicCalls, shortCalls, w.Body.String())
	}
	var response shareAPIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil { t.Fatal(err) }
	if shortCodeFromURL(t, response.URL) != "UniqueCode01" { t.Fatalf("url=%q", response.URL) }
	var snapshottedAt string
	if err := f.privDB.QueryRow(`SELECT snapshot->>'snapshotted_at' FROM article_shares WHERE public_id=$1`, response.ID).Scan(&snapshottedAt); err != nil { t.Fatal(err) }
	if snapshottedAt != "2026-09-08T12:00:00Z" { t.Fatalf("snapshotted_at=%q", snapshottedAt) }
}
```

Implement the terminal collision and historical fallback cases as:

```go
func TestCreateShareStopsAfterFiveCollisions(t *testing.T) {
	calls := 0
	f := newShareAPIFixtureWithOptions(t, api.ShareHandlerOptions{
		NewPublicID: func() (string, error) { calls++; return fmt.Sprintf("%032x", calls), nil },
		NewShortCode: func() (string, error) { return "Collision001", nil },
	})
	if _, err := f.privDB.Exec(`INSERT INTO article_shares(public_id,short_code,article_id,created_by,snapshot)
		VALUES($1,'Collision001',$2,$3,'{"title":"collision","url":"https://example.test","content":"body","snapshotted_at":"2026-09-01T00:00:00Z"}')`,
		strings.Repeat("f", 32), f.article, f.userA); err != nil { t.Fatal(err) }
	w := f.request(t, http.MethodPost, f.articlePath("/shares"), f.userA, `{"expires_at":null}`)
	if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"internal server error"}` || calls != 5 {
		t.Fatalf("status=%d calls=%d body=%q", w.Code, calls, w.Body.String())
	}
	var count int
	if err := f.privDB.QueryRow(`SELECT count(*) FROM article_shares`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestHistoricalShareManagementURLRemainsSigned(t *testing.T) {
	f := newShareAPIFixture(t)
	created := f.createShare(t, f.userA, `{"expires_at":null}`)
	if _, err := f.privDB.Exec(`UPDATE article_shares SET short_code=NULL WHERE public_id=$1`, created.ID); err != nil { t.Fatal(err) }
	w := f.request(t, http.MethodGet, f.articlePath("/shares"), f.userA, "")
	var rows []shareAPIResponse
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 1 { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
	if !strings.HasPrefix(rows[0].URL, "/share/v1_") { t.Fatalf("historical URL=%q", rows[0].URL) }
	value, legacy, err := f.signer.Parse(strings.TrimPrefix(rows[0].URL, "/share/"))
	if err != nil || legacy || value != created.ID { t.Fatalf("value=%q legacy=%v err=%v", value, legacy, err) }
}
```

For the rewrite test, include an external attack URL containing `/api/articles/<id>/images/0.png` and assert the short code never appears inside it.

- [ ] **Step 3: Run API tests and verify they fail**

```bash
cd backend
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/api -run 'Test(ShortShare|CreateShareRetries|CreateShareStops|HistoricalShare)' -count=1
```

Expected: build/test FAIL because short routing and retry behavior do not exist.

- [ ] **Step 4: Implement bounded creation and absolute management URLs**

In `Create`, build `snapshot := repository.SnapshotFromArticle(article, now)` once, then attempt at most five generated pairs:

```go
const maxShareCreateAttempts = 5

for attempt := 0; attempt < maxShareCreateAttempts; attempt++ {
	publicID, err := h.newPublicID()
	if err != nil { shareInternalError(c, "generate public ID", err); return }
	shortCode, err := h.newShortCode()
	if err != nil { shareInternalError(c, "generate short code", err); return }
	row, err := h.shares.WithCtx(c).CreateWithShortCode(article.ID, userID, publicID, shortCode, snapshot, expiresAt, now)
	if err != nil { shareInternalError(c, "create", err); return }
	if row != nil {
		c.JSON(http.StatusCreated, h.managementResponse(row, now))
		return
	}
}
shareInternalError(c, "create after unique collisions", errors.New("share identifier collision limit reached"))
```

In `managementResponse`, use `h.shortOrigin + "/" + *row.ShortCode` only when `ShortCode != nil`; otherwise preserve the exact signed long URL expression.

- [ ] **Step 5: Implement short snapshot and asset handlers**

Add `GetShortPublic` and `GetShortAsset`. Both reject non-Base62/non-12-byte values before repository access. Unlike the compatibility long-token route, a repository error on the new route returns the existing generic 500 response; missing/inactive rows return the existing generic 404.

Refactor asset rewriting to accept a caller-built prefix:

```go
func rewriteShareAssets(content, assetPrefix string, articleID int) string {
	return rewriteOutsideFencedCode(content, articleID, assetPrefix)
}
```

The long handler passes `"/api/share/" + url.PathEscape(token) + "/assets/"`; the short handler passes `"/api/s/" + shortCode + "/assets/"` only after validation.

- [ ] **Step 6: Validate origin and register production routes**

In `main`, validate before opening the public surface:

```go
shortOrigin, err := config.ValidateShortShareOrigin(cfg.Share.ShortOrigin)
if err != nil { log.Fatalf("invalid SHORT_SHARE_ORIGIN: %v", err) }

shareHandler := api.NewShareHandler(shareRepo, articleRepo, shareSigner, pdfImgHandler, api.ShareHandlerOptions{
	Now: time.Now, ShortOrigin: shortOrigin,
})
```

Register:

```go
router.GET("/api/s/:short_code", sharePageLimit, shareHandler.GetShortPublic)
router.GET("/api/s/:short_code/assets/:asset", shareAssetLimit, shareHandler.GetShortAsset)
```

Reuse the existing page/asset rate limiter instances so a client cannot multiply its allowance by switching URL formats.

- [ ] **Step 7: Verify new and historical APIs**

```bash
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/api ./internal/repository ./cmd/server -run 'Share|ShortCode|ShortShareOrigin' -count=1
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test -race ./internal/sharetoken ./internal/api ./internal/repository -run 'Share|ShortCode' -count=1
```

Expected: PASS, including the unchanged legacy/public-ID cases.

- [ ] **Step 8: Commit the public API slice**

```bash
git add backend/internal/api/share.go backend/internal/api/share_test.go backend/internal/api/share_rewrite_test.go backend/cmd/server/main.go
git commit -m "feat: serve article shares by short code"
```

### Task 5: Remove bearer values from application access and recovery logs

**Files:**
- Create: `backend/internal/api/access_log.go`
- Create: `backend/internal/api/access_log_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write redaction tests**

Use an in-memory writer and assert the logger records only Gin route templates:

```go
func TestRedactedAccessLoggerOmitsPathValuesAndQuery(t *testing.T) {
	var out bytes.Buffer
	r := gin.New()
	r.Use(RedactedAccessLogger(&out))
	r.GET("/api/s/:short_code", func(c *gin.Context) { c.Status(http.StatusNotFound) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/s/Aa0000000000?secret=query", nil))
	logText := out.String()
	if !strings.Contains(logText, "/api/s/:short_code") || strings.Contains(logText, "Aa0000000000") || strings.Contains(logText, "secret=query") {
		t.Fatalf("unsafe access log: %q", logText)
	}
}

func TestRedactedAccessLoggerUsesUnmatchedMarker(t *testing.T) {
	var out bytes.Buffer
	r := gin.New()
	r.Use(RedactedAccessLogger(&out))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/private-value", nil))
	if !strings.Contains(out.String(), "<unmatched>") || strings.Contains(out.String(), "private-value") {
		t.Fatalf("unsafe access log: %q", out.String())
	}
}

func TestRedactedRecoveryOmitsRequestAndPanicValues(t *testing.T) {
	var out bytes.Buffer
	r := gin.New()
	r.Use(RedactedRecovery(&out))
	r.GET("/api/s/:short_code", func(*gin.Context) { panic("Aa0000000000") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/s/Aa0000000000", nil))
	if w.Code != http.StatusInternalServerError || strings.Contains(out.String(), "Aa0000000000") {
		t.Fatalf("status=%d unsafe recovery log=%q", w.Code, out.String())
	}
}
```

- [ ] **Step 2: Run focused tests and verify the middleware is missing**

```bash
cd backend
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/api -run 'RedactedAccess|RedactedRecovery' -count=1
```

Expected: build FAIL.

- [ ] **Step 3: Implement template-only logging and value-free recovery**

`RedactedAccessLogger(io.Writer)` must record timestamp, status, latency, client IP, method, and `c.FullPath()` after `c.Next()`. If `FullPath()` is empty, write `<unmatched>`; never use `RequestURI`, `URL.Path`, `RawQuery`, parameters, headers, error strings, or bodies.

`RedactedRecovery(io.Writer)` must recover, write a constant `panic recovered` line plus `debug.Stack()`, and return 500. It must not format the recovered value or dump the request.

Replace:

```go
router := gin.Default()
```

with:

```go
router := gin.New()
router.Use(api.RedactedAccessLogger(gin.DefaultWriter), api.RedactedRecovery(gin.DefaultErrorWriter))
```

- [ ] **Step 4: Verify logging and server tests**

```bash
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./internal/api ./cmd/server -run 'Redacted|Share' -count=1
```

Expected: PASS and captured output contains route templates only.

- [ ] **Step 5: Commit the logging boundary**

```bash
git add backend/internal/api/access_log.go backend/internal/api/access_log_test.go backend/cmd/server/main.go
git commit -m "fix: redact bearer values from API logs"
```

### Task 6: Route the short hostname to the shared public reader

**Files:**
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/pages/SharePage.tsx`
- Modify: `frontend/src/components/PublicArticleReader.tsx`
- Modify: `frontend/src/components/MarkdownArticle.tsx`
- Create: `frontend/test/ShortShareRoutes.test.tsx`
- Modify: `frontend/test/SharePage.test.tsx`
- Modify: `frontend/test/MarkdownArticleAnchors.test.tsx`
- Modify: `frontend/test/ShareDialog.test.tsx`

- [ ] **Step 1: Write hostname-isolation tests**

Create `ShortShareRoutes.test.tsx` and assert:

```tsx
it('renders only a 12-character root share on the short hostname', async () => {
  render(<MemoryRouter initialEntries={['/Aa0000000000']}><ShortShareRoutes /></MemoryRouter>)
  await waitFor(() => expect(axios.get).toHaveBeenCalledWith('/api/s/Aa0000000000', expect.any(Object)))
})

it.each(['/login', '/articles', '/api/health', '/too-short', '/Aa000000000_'])('rejects %s without private API calls', path => {
  render(<MemoryRouter initialEntries={[path]}><ShortShareRoutes /></MemoryRouter>)
  expect(screen.getByText('分享链接无效或已过期')).toBeTruthy()
  expect(getMe).not.toHaveBeenCalled()
  expect(axios.get).not.toHaveBeenCalled()
})
```

Also retain a main-route test showing `/Aa0000000000` does not render a short share on `rss.morefreeze.top`.

- [ ] **Step 2: Add short-loader, CTA, asset, and canonical-URL tests**

In `SharePage.test.tsx`, render `<SharePage kind="short" />` at a root code and assert `/api/s/<code>`, no redirect, shared retry behavior, and the same document-title/referrer lifecycle as the long route.

In reader/Markdown tests, assert:

```tsx
expect(screen.getByRole('link', { name: '使用 RSS Pal' }).getAttribute('href'))
  .toBe('https://rss.morefreeze.top/login?intent=use&return_to=%2Farticles')
expect(screen.getByRole('link', { name: '订阅原始来源' }).getAttribute('href'))
  .toMatch(/^https:\/\/rss\.morefreeze\.top\/login\?intent=subscribe/)
expect(rendered.querySelector('img')?.getAttribute('src'))
  .toBe('/api/s/Aa0000000000/assets/0.png')
```

Add negative image cases for 11/13 characters, underscore, extra path segments, and external URLs containing a short API substring.

In `ShareDialog.test.tsx`, make the create mock return `https://r.morefreeze.top/Aa0000000000`; assert automatic clipboard copy receives that exact URL without rebasing it to the main origin, and assert the existing owner-side X intent carries the same URL.

- [ ] **Step 3: Run the focused frontend tests and verify failure**

```bash
cd frontend
npm test -- ShortShareRoutes.test.tsx SharePage.test.tsx MarkdownArticleAnchors.test.tsx
```

Expected: FAIL because `ShortShareRoutes`, `kind="short"`, absolute CTAs, and the short asset allow-list do not exist.

- [ ] **Step 4: Split the app shell by hostname before private hooks run**

Export:

```tsx
export const SHORT_SHARE_HOSTNAME = 'r.morefreeze.top'
export function isShortShareHostname(hostname: string) {
  return hostname.toLowerCase().replace(/\.$/u, '') === SHORT_SHARE_HOSTNAME
}
export function ShortShareRoutes() {
  return <Routes>
    <Route path="/:shortCode" element={<SharePage kind="short" />} />
    <Route path="*" element={<div className="public-reader-state card">分享链接无效或已过期</div>} />
  </Routes>
}
```

Move all current user state and `getMe()` hydration into `MainSiteApp`. `App` must choose `ShortShareRoutes` before mounting `MainSiteApp`, so a browser carrying main-site local storage never calls private endpoints on `r.morefreeze.top`.

- [ ] **Step 5: Reuse one long/short loader**

Give `SharePage` this prop and identifier selection:

```tsx
type SharePageProps = { kind?: 'long' | 'short' }

export default function SharePage({ kind = 'long' }: SharePageProps) {
  const { token, shortCode } = useParams<{ token?: string; shortCode?: string }>()
  const identifier = kind === 'short' ? shortCode : token
  const valid = Boolean(identifier) && (kind === 'long' || /^[0-9A-Za-z]{12}$/u.test(identifier!))
  const endpoint = valid
    ? kind === 'short'
      ? `/api/s/${identifier}`
      : `/api/share/${encodeURIComponent(identifier!)}`
    : null
  // Keep the existing abort, stale-response, retry, title, and referrer logic;
  // use endpoint as the effect dependency and request target.
}
```

Pass a canonical public URL to the reader rather than making it infer `/share/`:

```tsx
<PublicArticleReader article={article} shareURL={`${window.location.origin}${window.location.pathname}`} />
```

- [ ] **Step 6: Make CTAs absolute and keep the asset allow-list narrow**

Add `shareURL` to `PublicArticleReader` props. Build login and subscription hrefs against constant `https://rss.morefreeze.top`, preserving `authSearch` and safe-source parsing. Replace the image allow-list with a grouped exact regex:

```ts
const PUBLIC_SHARE_ASSET_RE = /^(?:\/api\/share\/(?:[A-Za-z0-9]{8}|v1_[0-9a-f]{32}_[A-Za-z0-9_-]{43})|\/api\/s\/[0-9A-Za-z]{12})\/assets\/[0-9]+\.(?:png|jpe?g)$/u
```

- [ ] **Step 7: Verify the complete public-reader surface**

```bash
npm test -- ShortShareRoutes.test.tsx SharePage.test.tsx ShareDialog.test.tsx MarkdownArticleAnchors.test.tsx ReaderInteractionSurface.test.tsx AuthIntentRoutes.test.tsx
```

Expected: PASS; main long routes, short root routes, CTA intent preservation, and image proxy behavior all remain covered.

- [ ] **Step 8: Commit the hostname-isolated reader**

```bash
git add frontend/src/App.tsx frontend/src/pages/SharePage.tsx frontend/src/components/PublicArticleReader.tsx frontend/src/components/MarkdownArticle.tsx frontend/test/ShortShareRoutes.test.tsx frontend/test/SharePage.test.tsx frontend/test/ShareDialog.test.tsx frontend/test/MarkdownArticleAnchors.test.tsx
git commit -m "feat: render shares on the short domain"
```

### Task 7: Enforce the confirmed 140-character X post budget

**Files:**
- Modify: `frontend/src/utils/xShare.ts`
- Modify: `frontend/test/XShare.test.ts`
- Modify: `frontend/test/SharePage.test.tsx`

- [ ] **Step 1: Change tests from 280 to the product limit**

Replace every hard-coded `280` assertion with `X_MAX_WEIGHT`, assert `X_MAX_WEIGHT === 140`, and add a short-domain case:

```ts
it('uses the short URL and maximizes summary within 140 weighted characters', () => {
  const text = buildXPostText({
    title: '中文标题',
    summaryBrief: `重点总结${'内容'.repeat(100)}`,
    shareURL: 'https://r.morefreeze.top/Aa0000000000',
  })
  expect(text).toContain('重点总结')
  expect(text.endsWith('https://r.morefreeze.top/Aa0000000000')).toBe(true)
  expect(xWeightedLength(text)).toBeLessThanOrEqual(140)
})
```

In the short `SharePage` integration test, click “分享到 X” and assert the `https://x.com/intent/post` `text` parameter contains title, summary prefix, and the canonical short URL, with `xWeightedLength(text) <= 140`.

- [ ] **Step 2: Run X tests and verify the old limit fails**

```bash
cd frontend
npm test -- XShare.test.ts SharePage.test.tsx
```

Expected: FAIL because `X_MAX_WEIGHT` is still 280.

- [ ] **Step 3: Set the single source of truth to 140**

Change only:

```ts
export const X_MAX_WEIGHT = 140
```

Keep URL weighting, grapheme-safe truncation, Markdown stripping, title-first priority, summary fallback, and URL preservation unchanged.

- [ ] **Step 4: Verify X composition**

```bash
npm test -- XShare.test.ts SharePage.test.tsx ShareDialog.test.tsx
```

Expected: PASS and every generated public-reader post is ready for the final send click.

- [ ] **Step 5: Commit the X budget correction**

```bash
git add frontend/src/utils/xShare.ts frontend/test/XShare.test.ts frontend/test/SharePage.test.tsx
git commit -m "fix: constrain X share posts to 140 characters"
```

### Task 8: Restrict the short domain at container Nginx and wire deployment configuration

**Files:**
- Modify: `frontend/nginx.conf`
- Create: `frontend/test/nginxShortShareHost.test.cjs`
- Modify: `frontend/test/nginxMediaRelay.test.cjs`
- Modify: `scripts/auto_deploy.sh`
- Modify: `scripts/tests/auto_deploy_service_selection_test.sh`
- Create: `scripts/tests/auto_deploy_direct_network_test.sh`
- Modify: `.env.example`
- Modify: `docker-compose.yml`
- Modify: `.github/workflows/deploy-tencent.yml`
- Modify: `README.md`

- [ ] **Step 1: Write static Nginx and Compose tests**

The new Node legacy test must extract the `server_name r.morefreeze.top;` block and assert all of the following literals or equivalent exact regex locations are present:

```js
[
  'access_log off;',
  'location ^~ /api/s/',
  'location = /api/proxy/image',
  'location ^~ /assets/',
  'location = /favicon.svg',
  'location = /favicon-32.png',
  'location = /apple-touch-icon.png',
  'location ~ "^/[0-9A-Za-z]{12}$"',
  'location /',
  'return 404;',
]
```

It must also assert the short block does not contain generic `location ^~ /api {`, `/login`, `/articles`, `/status`, or `proxy_pass` outside the explicit public locations. Extend the existing media test to select the main-domain server block rather than the whole file.

Add a shell/static assertion that `status-migrate` runs `041_article_share_short_codes.sql`, the API receives required `SHORT_SHARE_ORIGIN`, and the GitHub Action short-domain curls include `--noproxy '*'`.

- [ ] **Step 2: Run legacy tests and verify failure**

```bash
cd frontend
npm run test:legacy
```

Expected: FAIL because the short virtual host is absent.

- [ ] **Step 3: Add a separate short-domain server block**

Keep the existing main-domain routes under `server_name localhost rss.morefreeze.top;`. Set `access_log off;` there so historical long bearer tokens cannot reach raw Nginx access logs.

Add `server_name r.morefreeze.top;` on the same internal listeners and TLS development certificate. This block must:

- proxy only `/api/s/` and exact `/api/proxy/image` to `api:8080`;
- serve `/assets/` and the three favicon paths from the built bundle;
- serve `index.html` with `no-store`, `Referrer-Policy: no-referrer`, and `X-Content-Type-Options: nosniff` only for `location ~ "^/[0-9A-Za-z]{12}$"`;
- return 404 for every other path;
- set `access_log off;` so even valid short codes are not logged raw.

Use `^~ /api/s/` before the static extension regex so share assets ending in `.png` reach the API.

- [ ] **Step 4: Wire migration and required origin into Compose**

Append migration 041 to the one-shot `status-migrate` command and add:

```yaml
SHORT_SHARE_ORIGIN: ${SHORT_SHARE_ORIGIN:?SHORT_SHARE_ORIGIN required in .env}
```

to the API environment only. Add this production example:

```dotenv
# Public origin used for newly created short article shares; no trailing slash.
SHORT_SHARE_ORIGIN=https://r.morefreeze.top
```

Update README migration documentation through 041 and state that the short domain is a public bearer surface, not a second authenticated app host.

- [ ] **Step 5: Add direct-network GitHub Action checks**

Keep the deploy command unchanged. Add a post-deploy step that uses direct connections explicitly:

```yaml
- name: Verify Tencent short share surface
  run: |
    test "$(curl --noproxy '*' --resolve r.morefreeze.top:443:192.144.171.125 \
      -sS -o /dev/null -w '%{http_code}' https://r.morefreeze.top/Aa0000000000)" = "200"
    test "$(curl --noproxy '*' --resolve r.morefreeze.top:443:192.144.171.125 \
      -sS -o /dev/null -w '%{http_code}' https://r.morefreeze.top/login)" = "404"
    test "$(curl --noproxy '*' --resolve r.morefreeze.top:443:192.144.171.125 \
      -sS -o /dev/null -w '%{http_code}' https://r.morefreeze.top/api/s/not-valid)" = "404"
```

These checks deliberately do not use Mihomo, `HTTPS_PROXY`, or the OCI scraping tunnel.

- [ ] **Step 6: Make Actions deployment itself direct and frontend-first**

Add a direct mode at the start of `configure_outbound_proxy`:

```bash
if [ "${RSS_PAL_DEPLOY_DIRECT:-0}" = "1" ]; then
  unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY all_proxy ALL_PROXY
  export no_proxy='*'
  export NO_PROXY='*'
  log "Deployment network mode: direct"
  return
fi
```

Guard the `rss-pal-oci-egress.service` restart/wait block with `[ "${RSS_PAL_DEPLOY_DIRECT:-0}" != "1" ]`. This changes only the deployment process: retain `configure_compose_files` and its OCI override so API/worker scraping containers continue receiving their configured egress proxy.

In `deploy_runtime_services`, build first, start any changed frontend with `up -d --no-deps frontend`, and only then run the dependency-aware API/worker `up`. For `DEPLOY_ALL`, use this exact order:

```bash
$COMPOSE "${COMPOSE_FILES[@]}" build || return
$COMPOSE "${COMPOSE_FILES[@]}" up -d --no-deps frontend || return
$COMPOSE "${COMPOSE_FILES[@]}" up -d || return
```

Update `auto_deploy_service_selection_test.sh` to compare the captured command line numbers and assert frontend `up` precedes backend/all-services `up`. Create `auto_deploy_direct_network_test.sh` by extracting `configure_outbound_proxy`, mocking `curl`, setting every upper/lower proxy variable, and setting `RSS_PAL_DEPLOY_DIRECT=1`. Assert curl calls remain zero, all proxy variables are unset, both no-proxy variables equal `*`, and logs contain `Deployment network mode: direct`.

- [ ] **Step 7: Verify Nginx, Compose rendering, frontend build, and deployment isolation**

```bash
npm run test:legacy
npm run build
cd ..
SHORT_SHARE_ORIGIN=https://r.morefreeze.top DB_PASSWORD=test AUTH_PASSWORD=test JWT_SECRET=test SHARE_SECRET=0123456789abcdef0123456789abcdef docker compose config >/tmp/rss-pal-short-share-compose.yml
rg -n '041_article_share_short_codes|SHORT_SHARE_ORIGIN: https://r.morefreeze.top' /tmp/rss-pal-short-share-compose.yml
bash scripts/tests/auto_deploy_service_selection_test.sh
bash scripts/tests/auto_deploy_proxy_ready_test.sh
bash scripts/tests/auto_deploy_direct_network_test.sh
```

Expected: legacy tests and build PASS; rendered Compose contains migration 041 and the exact production short origin; direct mode never probes a proxy; normal scraping-proxy readiness tests remain unchanged.

- [ ] **Step 8: Commit the container deployment contract**

```bash
git add frontend/nginx.conf frontend/test/nginxShortShareHost.test.cjs frontend/test/nginxMediaRelay.test.cjs scripts/auto_deploy.sh scripts/tests/auto_deploy_service_selection_test.sh scripts/tests/auto_deploy_direct_network_test.sh .env.example docker-compose.yml .github/workflows/deploy-tencent.yml README.md
git commit -m "feat: restrict and deploy the short share host"
```

### Task 9: Add reviewable Tencent host Nginx templates

**Files:**
- Create: `deploy/nginx/r-morefreeze-bootstrap.conf`
- Create: `deploy/nginx/rss-pal-tencent.conf`
- Create: `deploy/tencent/rss-pal-deploy-from-actions`
- Create: `scripts/tests/tencent_short_domain_nginx_test.sh`

- [ ] **Step 1: Write the failing host-template test**

The script must fail unless:

- bootstrap listens on 80 for only `r.morefreeze.top`, serves only `/.well-known/acme-challenge/` from `/var/www/html`, and returns 404 for everything else;
- final config contains separate HTTP redirect and TLS servers for `rss.morefreeze.top` and `r.morefreeze.top`;
- final short server references `/etc/letsencrypt/live/r.morefreeze.top/fullchain.pem` and `privkey.pem`;
- both TLS servers use `access_log off;`;
- the main TLS host proxies its existing complete surface to `http://127.0.0.1:8082`;
- the short TLS host proxies only `/api/s/`, exact `/api/proxy/image`, `/assets/`, the favicon files, and a quoted 12-character root regex to `127.0.0.1:8082`, with Host, real IP, forwarded-for, and forwarded-proto headers; its catch-all returns 404.
- the Actions wrapper retains the installed lock/fetch behavior but invokes the fetched deployment script through `env RSS_PAL_DEPLOY_DIRECT=1`; it must not define a proxy URL.

Run:

```bash
bash scripts/tests/tencent_short_domain_nginx_test.sh
```

Expected: FAIL because the templates do not exist.

- [ ] **Step 2: Create bootstrap and final configurations**

The bootstrap file must be a single HTTP server suitable for Certbot's webroot authenticator and must not expose the old full application while the short-domain bundle is not deployed:

```nginx
server {
    listen 80;
    server_name r.morefreeze.top;
    access_log off;
    location ^~ /.well-known/acme-challenge/ {
        root /var/www/html;
    }
    location / { return 404; }
}
```

The final file must preserve the main host's current 5 MiB body limit and 60-second read timeout, add the short host with its own Let's Encrypt certificate, redirect both HTTP hosts to HTTPS, and disable raw access logs on both TLS servers. The main host retains its catch-all proxy. The short host duplicates the explicit path allow-list from Task 8 and returns 404 for its catch-all, so it stays safe even if the container configuration regresses. Do not commit certificate bytes or private keys.

Create `deploy/tencent/rss-pal-deploy-from-actions` from the currently installed wrapper, retaining `flock`, cleanup, `git fetch`, and `git show`. Both the bootstrap `git fetch` and fetched script must run with proxy variables removed and `no_proxy='*'`. Use these exact invocations:

```bash
sudo -H -u ubuntu env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u all_proxy -u ALL_PROXY no_proxy='*' NO_PROXY='*' bash -lc "cd '$REPO' && git fetch origin master && git show origin/master:scripts/auto_deploy.sh > '$SCRIPT'"
sudo -H -u ubuntu env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY -u all_proxy -u ALL_PROXY no_proxy='*' NO_PROXY='*' RSS_PAL_DEPLOY_DIRECT=1 bash -lc "cd '$REPO' && bash '$SCRIPT'"
```

Extend the shell test to assert both direct environment handoffs and to reject wrapper text that assigns a proxy URL.

- [ ] **Step 3: Verify templates**

```bash
bash scripts/tests/tencent_short_domain_nginx_test.sh
```

Expected: PASS.

- [ ] **Step 4: Commit the host-infrastructure artifacts**

```bash
git add deploy/nginx/r-morefreeze-bootstrap.conf deploy/nginx/rss-pal-tencent.conf deploy/tencent/rss-pal-deploy-from-actions scripts/tests/tencent_short_domain_nginx_test.sh
git commit -m "chore: define Tencent short domain ingress"
```

### Task 10: Full local verification and review

**Files:**
- Review all files changed since `1d80989`

- [ ] **Step 1: Run formatting and static checks**

```bash
gofmt -w backend/internal/model/article_share.go backend/internal/sharetoken/shortcode.go backend/internal/sharetoken/shortcode_test.go backend/internal/config/config.go backend/internal/config/share_test.go backend/internal/repository/share.go backend/internal/repository/share_test.go backend/internal/repository/share_short_code_migration_test.go backend/internal/api/share.go backend/internal/api/share_test.go backend/internal/api/share_rewrite_test.go backend/internal/api/access_log.go backend/internal/api/access_log_test.go backend/cmd/server/main.go
git diff --check
```

Expected: no output from `git diff --check`.

- [ ] **Step 2: Run full backend verification**

Use an execution environment that permits local `httptest` listeners:

```bash
cd backend
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test ./... -count=1
GOCACHE=/tmp/rss-pal-short-share-go-build-cache /Users/bytedance/homebrew/bin/go test -race ./internal/sharetoken ./internal/api ./internal/repository -count=1
```

Expected: all packages PASS.

- [ ] **Step 3: Run full frontend and infrastructure verification**

```bash
cd ../frontend
npm run check
npm run build
cd ..
bash scripts/tests/tencent_short_domain_nginx_test.sh
bash scripts/tests/auto_deploy_service_selection_test.sh
bash scripts/tests/auto_deploy_proxy_ready_test.sh
bash scripts/tests/auto_deploy_direct_network_test.sh
```

Expected: `55+` Vitest files, all legacy tests, build, and shell tests PASS.

- [ ] **Step 4: Inspect the integrated diff**

```bash
git status --short
git diff 1d80989 --stat
git diff 1d80989 -- backend/migrations backend/internal/sharetoken backend/internal/config backend/internal/repository/share.go backend/internal/api/share.go backend/internal/api/access_log.go backend/cmd/server/main.go
git diff 1d80989 -- frontend/src/App.tsx frontend/src/pages/SharePage.tsx frontend/src/components/PublicArticleReader.tsx frontend/src/components/MarkdownArticle.tsx frontend/src/utils/xShare.ts frontend/nginx.conf
git diff 1d80989 -- docker-compose.yml .github/workflows/deploy-tencent.yml deploy/nginx deploy/tencent scripts/auto_deploy.sh scripts/tests README.md .env.example
```

Expected: no files outside the locked map and no generated artifacts, secrets, certificate bytes, or private keys.

- [ ] **Step 5: Request code review and fix all findings**

Invoke `superpowers:requesting-code-review` against `1d80989..HEAD`. Re-run the exact focused suite for each accepted fix, then repeat Steps 1-3.

- [ ] **Step 6: Commit any verification-only corrections**

If review required tracked corrections, stage only those named files and commit:

```bash
git commit -m "fix: address short share review findings"
```

If no corrections were required, do not create an empty commit.

### Task 11: Provision and deploy the production short domain

**External state:**
- DNS record for `r.morefreeze.top`
- Tencent files `/etc/nginx/sites-available/rss-pal` and `/etc/nginx/sites-enabled/rss-pal`
- Tencent Actions wrapper `/usr/local/sbin/rss-pal-deploy-from-actions`
- Let's Encrypt certificate `/etc/letsencrypt/live/r.morefreeze.top/`
- GitHub `master` deployment workflow

Current verified precondition on 2026-09-13: `rss.morefreeze.top` resolves to `192.144.171.125`; `r.morefreeze.top` has no A record; the Tencent Certbot inventory covers only `rss.morefreeze.top` and `theater.morefreeze.top`.

- [ ] **Step 1: Create and verify the DNS record**

Create an A record `r.morefreeze.top -> 192.144.171.125` with the same public-DNS provider used for the main domain. Verify direct DNS:

```bash
dig +short r.morefreeze.top A
```

Expected: exactly `192.144.171.125` from public resolvers before certificate issuance.

- [ ] **Step 2: Install a recoverable HTTP bootstrap on Tencent**

Copy the current config and install the reviewed bootstrap as a separate enabled site:

```bash
scp -o ProxyJump=oci-rss-pal deploy/nginx/r-morefreeze-bootstrap.conf tencent-rss-pal:/tmp/r-morefreeze-bootstrap.conf
ssh -o ControlMaster=no -o ControlPath=none -o ProxyJump=oci-rss-pal tencent-rss-pal 'sudo cp /etc/nginx/sites-available/rss-pal /etc/nginx/sites-available/rss-pal.pre-short-domain && sudo install -m 0644 /tmp/r-morefreeze-bootstrap.conf /etc/nginx/sites-available/r-morefreeze-bootstrap && sudo ln -sfn /etc/nginx/sites-available/r-morefreeze-bootstrap /etc/nginx/sites-enabled/r-morefreeze-bootstrap && sudo nginx -t && sudo systemctl reload nginx'
```

Expected: `nginx -t` succeeds; `http://r.morefreeze.top/` returns 404 while `/.well-known/acme-challenge/` is available to Certbot.

- [ ] **Step 3: Issue the independent certificate**

```bash
ssh -o ControlMaster=no -o ControlPath=none -o ProxyJump=oci-rss-pal tencent-rss-pal 'sudo mkdir -p /var/www/html/.well-known/acme-challenge && sudo certbot certonly --webroot -w /var/www/html --non-interactive --agree-tos -d r.morefreeze.top && sudo certbot certificates'
```

Expected: a valid certificate whose Domains line is exactly `r.morefreeze.top`; no private key is printed or copied locally.

- [ ] **Step 4: Install the final reviewed virtual hosts**

```bash
scp -o ProxyJump=oci-rss-pal deploy/nginx/rss-pal-tencent.conf tencent-rss-pal:/tmp/rss-pal-tencent.conf
ssh -o ControlMaster=no -o ControlPath=none -o ProxyJump=oci-rss-pal tencent-rss-pal 'sudo install -m 0644 /tmp/rss-pal-tencent.conf /etc/nginx/sites-available/rss-pal && sudo rm -f /etc/nginx/sites-enabled/r-morefreeze-bootstrap && sudo nginx -t && sudo systemctl reload nginx'
```

Expected: both hostnames negotiate their own certificate, both proxy to the frontend on `127.0.0.1:8082`, and host Nginx writes no raw share URI.

- [ ] **Step 5: Add production environment before application deployment**

On Tencent, add exactly this non-secret value to `/opt/rss-pal/.env` without displaying the rest of the file:

```dotenv
SHORT_SHARE_ORIGIN=https://r.morefreeze.top
```

Verify only the key name and expected value, never dump the complete `.env`.

- [ ] **Step 6: Install the reviewed direct-network Actions wrapper**

Back up and replace only the existing wrapper:

```bash
scp -o ProxyJump=oci-rss-pal deploy/tencent/rss-pal-deploy-from-actions tencent-rss-pal:/tmp/rss-pal-deploy-from-actions
ssh -o ControlMaster=no -o ControlPath=none -o ProxyJump=oci-rss-pal tencent-rss-pal 'sudo cp /usr/local/sbin/rss-pal-deploy-from-actions /usr/local/sbin/rss-pal-deploy-from-actions.pre-direct && sudo install -o root -g root -m 0755 /tmp/rss-pal-deploy-from-actions /usr/local/sbin/rss-pal-deploy-from-actions && sudo grep -F "RSS_PAL_DEPLOY_DIRECT=1" /usr/local/sbin/rss-pal-deploy-from-actions'
```

Expected: the wrapper retains its deployment lock and origin/master fetch, and the inner `ubuntu` process receives direct mode. This does not remove or restart `rss-pal-oci-egress.service`; feed/article fetching continues using the Compose egress override.

- [ ] **Step 7: Integrate with `master` and let GitHub Actions deploy directly**

Use `superpowers:finishing-a-development-branch` to integrate `codex/short-share-domain` only after all local checks pass. Push the resulting `master`; do not rewrite history. The self-hosted Tencent deployment must use the existing action and its explicit `curl --noproxy '*'` checks, not Mihomo transparent proxying.

- [ ] **Step 8: Verify deployed revision and runtime state**

Read back the GitHub Action result and verify on Tencent:

```bash
cd /opt/rss-pal
git rev-parse HEAD
docker compose ps
docker inspect rss-pal-api-1 --format '{{.State.Status}}'
docker inspect rss-pal-frontend-1 --format '{{.State.Status}}'
curl --noproxy '*' -fsS http://127.0.0.1:8080/api/health
curl --noproxy '*' --resolve rss.morefreeze.top:443:192.144.171.125 -fsS https://rss.morefreeze.top/api/health
```

Expected: deployed revision equals pushed `master`, both containers are `running`, and direct/local health responses are successful.

- [ ] **Step 9: Perform real public acceptance**

Using an authenticated main-site session, create a new share and record only its shape, not the bearer value, in logs or the handoff. In a fresh browser profile:

1. Open the returned `https://r.morefreeze.top/<12 Base62>` URL.
2. Confirm the address bar never changes and the immutable title/body/assets/media load.
3. Confirm “使用 RSS Pal” and “订阅原始来源” go to main-site `https://rss.morefreeze.top/login` intent URLs rather than a login route on the short host.
4. Confirm “分享到 X” opens `https://x.com/intent/post`, contains title, as much summary as fits, and the short URL, with a weighted length no greater than 140.
5. Revoke the share from the owner UI and confirm both page and local asset API return the generic unavailable state.
6. Open a known historical long share and confirm it still works.

- [ ] **Step 10: Verify bearer values did not reach logs**

Search application and both Nginx access logs using only a securely held test code, then discard it from the shell history. The expected count is zero in raw logs; the application access log should contain only `/api/s/:short_code` and `/api/share/:token` templates. Do not paste the code or matching log lines into the final report.

- [ ] **Step 11: Verify the rollback path without executing it**

Confirm `/etc/nginx/sites-available/rss-pal.pre-short-domain` and `/usr/local/sbin/rss-pal-deploy-from-actions.pre-direct` exist, and `git show 1d80989:docker-compose.yml` demonstrates that the prior application ignores the additive migration column. If production verification fails, execute this recovery sequence instead of dropping migration 041:

```bash
ssh -o ControlMaster=no -o ControlPath=none -o ProxyJump=oci-rss-pal tencent-rss-pal 'sudo cp /etc/nginx/sites-available/rss-pal.pre-short-domain /etc/nginx/sites-available/rss-pal && sudo cp /usr/local/sbin/rss-pal-deploy-from-actions.pre-direct /usr/local/sbin/rss-pal-deploy-from-actions && sudo nginx -t && sudo systemctl reload nginx'
```

Then redeploy the previous application revision through the existing deployment mechanism. Keep the nullable `short_code` column and index in place because the previous binary ignores them; do not run destructive down-migrations. Revoke the acceptance-test share before rollback. Remove the DNS record only after the restored main host is healthy and the short host is confirmed unavailable.

- [ ] **Step 12: Report the delivery boundary**

Report the pushed commit, GitHub Action URL/result, Tencent deployed revision, migration 041 presence, container states, local/direct/public health, short-page final URL behavior, X weighted-length result, revoked-resource result, historical-link result, and log-redaction result. If any one is unavailable, label that boundary as unverified rather than treating deployment as complete.
