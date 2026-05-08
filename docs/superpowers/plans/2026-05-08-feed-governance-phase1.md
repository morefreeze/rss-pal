# Feed Governance Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a feed health dashboard at `/feeds/health` driven by trustworthy behavioral telemetry (exposure ≥10s / click / completed_read), surface 5 categories of pruning suggestions, and let the user pause/archive/de-weight feeds.

**Architecture:** New `article_events` event table backs all feed-health metrics. `feeds` table gains `status` (active/paused/archived) + `priority_weight` columns; legacy `is_active` is kept double-written during transition. A new service `feed_health.go` aggregates per-feed metrics with a single windowed CTE query. Frontend adds an `useExposureTracking` hook (IntersectionObserver + ≥10s timer) wired into existing list cards, and a new `/feeds/health` page with KPI cards + sortable table + pruning drawer.

**Tech Stack:** Go (gin, database/sql, lib/pq), PostgreSQL 15, React 18 + TypeScript + Vite, axios, react-router-dom v6.

**Spec reference:** `docs/superpowers/specs/2026-05-08-feed-governance-design.md`

---

## File Map

**New files:**

Backend:
- `backend/migrations/015_feed_governance.sql` — schema migration
- `backend/internal/repository/event.go` — article_events repo
- `backend/internal/repository/feed_health.go` — feed health aggregation queries
- `backend/internal/service/feed_health.go` — metrics composition + pruning rules
- `backend/internal/service/feed_health_test.go` — unit tests for pruning rules + value score
- `backend/internal/config/feedhealth.go` — pruning thresholds
- `backend/internal/api/event.go` — POST /api/events handler
- `backend/internal/api/event_test.go` — handler validation tests
- `backend/internal/api/feed_health.go` — GET /api/feeds/health handler

Frontend:
- `frontend/src/hooks/useExposureTracking.ts` — IntersectionObserver + 10s timer hook
- `frontend/src/hooks/useExposureTracking.test.ts` — (skipped — no test framework yet, see Task F1 note)
- `frontend/src/pages/FeedHealthPage.tsx` — main dashboard
- `frontend/src/components/FeedHealthTable.tsx` — sortable table
- `frontend/src/components/FeedHealthKPI.tsx` — top KPI cards
- `frontend/src/components/PruningDrawer.tsx` — suggestion drawer

**Modified files:**
- `backend/internal/model/model.go` — Feed struct (add Status, PriorityWeight)
- `backend/internal/repository/feed.go` — scan helpers + new UpdateStatus/UpdateWeight + active query migration
- `backend/internal/api/feed.go` — add Status/Weight handlers + register routes via main.go
- `backend/internal/api/preference.go` — ProgressHandler.Update writes completed_read event on first false→true flip
- `backend/cmd/server/main.go` — register new routes
- `backend/cmd/worker/main.go` — change feed query from is_active to status='active' (transitive — repo method swap)
- `frontend/src/api/client.ts` — add event/health/feed-status helpers
- `frontend/src/App.tsx` — add `/feeds/health` route
- `frontend/src/pages/FeedListPage.tsx` — add "健康度 →" button
- `frontend/src/pages/ArticleListPage.tsx` — wire exposure tracking + click event
- `frontend/src/pages/ArticlePage.tsx` — add stay-time gate to completion

---

## Task 1: Database migration — events table + feeds columns

**Files:**
- Create: `backend/migrations/015_feed_governance.sql`

- [ ] **Step 1: Write the migration**

Create `backend/migrations/015_feed_governance.sql`:

```sql
-- 015_feed_governance.sql
-- Phase 1 feed governance: behavioral events + feed status/weight

CREATE TABLE article_events (
    id           BIGSERIAL PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id   INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    event_type   VARCHAR(32) NOT NULL,
    occurred_at  TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_article_events_user_time ON article_events (user_id, occurred_at DESC);
CREATE INDEX idx_article_events_article ON article_events (article_id);
CREATE INDEX idx_article_events_type ON article_events (event_type);

-- Feeds: status state machine + priority weight
ALTER TABLE feeds ADD COLUMN status VARCHAR(16) NOT NULL DEFAULT 'active';
ALTER TABLE feeds ADD COLUMN priority_weight DOUBLE PRECISION NOT NULL DEFAULT 1.0;

-- Conservative migration: existing inactive feeds → paused, active → active
UPDATE feeds SET status = 'paused' WHERE is_active = false;
UPDATE feeds SET status = 'active' WHERE is_active = true;

CREATE INDEX idx_feeds_status ON feeds (status);
```

- [ ] **Step 2: Run migration locally**

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -f - < backend/migrations/015_feed_governance.sql
```

Expected: `CREATE TABLE`, `CREATE INDEX` (×3), `ALTER TABLE` (×2), `UPDATE` rows, `CREATE INDEX` no errors.

- [ ] **Step 3: Verify schema**

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "\\d article_events" && \
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "\\d feeds" | grep -E "status|priority_weight"
```

Expected: article_events columns visible, feeds shows `status` and `priority_weight`.

- [ ] **Step 4: Commit**

```bash
git add backend/migrations/015_feed_governance.sql
git commit -m "feat(db): migration 015 — article_events table + feeds.status/priority_weight"
```

---

## Task 2: Update Feed model with new columns

**Files:**
- Modify: `backend/internal/model/model.go` — Feed struct
- Modify: `backend/internal/repository/feed.go` — both scan helpers

- [ ] **Step 1: Add fields to Feed struct**

In `backend/internal/model/model.go`, find `type Feed struct {` (~line 5) and add two fields before `CreatedAt`:

```go
type Feed struct {
	ID               int        `json:"id" db:"id"`
	URL              string     `json:"url" db:"url"`
	Title            string     `json:"title" db:"title"`
	LastFetchedAt    *time.Time `json:"last_fetched_at" db:"last_fetched_at"`
	FetchIntervalMin int        `json:"fetch_interval_minutes" db:"fetch_interval_minutes"`
	ETag             string     `json:"etag" db:"etag"`
	LastModified     string     `json:"last_modified" db:"last_modified"`
	IsActive         bool       `json:"is_active" db:"is_active"`
	OwnerID          *int       `json:"owner_id" db:"owner_id"`
	FeedType         string     `json:"feed_type" db:"feed_type"` // "rss" or "html"
	Status           string     `json:"status" db:"status"`
	PriorityWeight   float64    `json:"priority_weight" db:"priority_weight"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	ArticleCount     int        `json:"article_count" db:"article_count"`
	UnreadCount      int        `json:"unread_count" db:"unread_count"`
}
```

- [ ] **Step 2: Update scanFeed helper**

In `backend/internal/repository/feed.go` lines 17-39, replace `scanFeed` to include status and priority_weight:

```go
func (r *FeedRepository) scanFeed(row *sql.Row) (*model.Feed, error) {
	var f model.Feed
	var title, etag, lastModified, feedType, status sql.NullString
	var ownerID sql.NullInt64
	err := row.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	f.Title = title.String
	f.ETag = etag.String
	f.LastModified = lastModified.String
	f.FeedType = feedType.String
	if f.FeedType == "" {
		f.FeedType = "rss"
	}
	f.Status = status.String
	if f.Status == "" {
		f.Status = "active"
	}
	if ownerID.Valid {
		oid := int(ownerID.Int64)
		f.OwnerID = &oid
	}
	return &f, nil
}
```

- [ ] **Step 3: Update scanFeeds helper**

In `backend/internal/repository/feed.go` lines 41-65, replace `scanFeeds` similarly:

```go
func (r *FeedRepository) scanFeeds(rows *sql.Rows) ([]model.Feed, error) {
	var feeds []model.Feed
	for rows.Next() {
		var f model.Feed
		var title, etag, lastModified, feedType, status sql.NullString
		var ownerID sql.NullInt64
		err := rows.Scan(&f.ID, &f.URL, &title, &f.LastFetchedAt, &f.FetchIntervalMin, &etag, &lastModified, &f.IsActive, &ownerID, &feedType, &status, &f.PriorityWeight, &f.CreatedAt)
		if err != nil {
			return nil, err
		}
		f.Title = title.String
		f.ETag = etag.String
		f.LastModified = lastModified.String
		f.FeedType = feedType.String
		if f.FeedType == "" {
			f.FeedType = "rss"
		}
		f.Status = status.String
		if f.Status == "" {
			f.Status = "active"
		}
		if ownerID.Valid {
			oid := int(ownerID.Int64)
			f.OwnerID = &oid
		}
		feeds = append(feeds, f)
	}
	return feeds, nil
}
```

- [ ] **Step 4: Update SELECT lists in feed.go**

The scan helpers now expect `status` and `priority_weight` columns. Update every `SELECT ... FROM feeds` query in `backend/internal/repository/feed.go` to include them in the same position (after `feed_type`, before `created_at`). Affected lines: 68, 79, 85, 163, 183.

For each, replace:
```
... is_active, owner_id, feed_type, created_at ...
```
with:
```
... is_active, owner_id, feed_type, status, priority_weight, created_at ...
```

Also update the `GetVisibleByUser` SELECT (around line 85) which has a longer list:
```go
query := `
    SELECT f.id, f.url, f.title, f.last_fetched_at, f.fetch_interval_minutes, f.etag, f.last_modified, f.is_active, f.owner_id, f.feed_type, f.status, f.priority_weight, f.created_at,
           COUNT(a.id) AS article_count,
           COUNT(CASE WHEN COALESCE(rp.is_completed, false) = false THEN 1 END) AS unread_count
    FROM feeds f
    LEFT JOIN articles a ON a.feed_id = f.id
    LEFT JOIN reading_progress rp ON rp.article_id = a.id AND rp.user_id = $1
    WHERE f.owner_id IS NULL OR f.owner_id = $1
    GROUP BY f.id
    ORDER BY f.created_at DESC
`
```

The corresponding scan in `GetVisibleByUser` also needs to include the new columns. Read the current `Scan(...)` call inside that function and add `&f.Status, &f.PriorityWeight` between `&feedType` and `&f.CreatedAt`. Match the pattern used in scanFeeds: declare `var status sql.NullString`, scan into it, then set `f.Status`.

- [ ] **Step 5: Build to verify**

Run from repo root:
```bash
cd backend && go build ./... && cd ..
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/model/model.go backend/internal/repository/feed.go
git commit -m "feat(feed): add status + priority_weight to Feed model and queries"
```

---

## Task 3: Config — feed health thresholds

**Files:**
- Create: `backend/internal/config/feedhealth.go`

- [ ] **Step 1: Create config file**

Create `backend/internal/config/feedhealth.go`:

```go
package config

import "time"

// FeedHealthConfig holds thresholds for feed health metrics and pruning rules.
// Self-use, hardcoded — change values and restart to tune.
type FeedHealthConfig struct {
	DormantClickWindow          time.Duration // R2 沉睡型: 该窗口内 0 点击
	DormantMinArticles          int           // R2: 至少要有这么多文章才考虑沉睡判定
	DeadFeedArticleWindow       time.Duration // R3 死源型: 该窗口内 0 文章
	FullyDeadWindow             time.Duration // R1 完全失效: 该窗口同时 0 文章 0 点击
	LowValueScoreThreshold      float64       // R4 低价值: value_score 低于此值
	LowValueMinSampleSize       int           // R4: 至少这么多文章才参与低价值判定
	HighVolumeArticleCount      int           // R5 过水型: 30d 文章数 >此值
	HighVolumeMaxCompletionRate float64       // R5: 完读率低于此值
	ColdStartMinExposures       int           // 曝光数 < 此值时 value_score 为 null
}

func DefaultFeedHealth() FeedHealthConfig {
	return FeedHealthConfig{
		DormantClickWindow:          30 * 24 * time.Hour,
		DormantMinArticles:          3,
		DeadFeedArticleWindow:       30 * 24 * time.Hour,
		FullyDeadWindow:             90 * 24 * time.Hour,
		LowValueScoreThreshold:      0.1,
		LowValueMinSampleSize:       10,
		HighVolumeArticleCount:      100,
		HighVolumeMaxCompletionRate: 0.05,
		ColdStartMinExposures:       10,
	}
}
```

- [ ] **Step 2: Build**

```bash
cd backend && go build ./... && cd ..
```
Expected: clean build.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/config/feedhealth.go
git commit -m "feat(config): add FeedHealthConfig with default thresholds"
```

---

## Task 4: ArticleEvent model + repository

**Files:**
- Modify: `backend/internal/model/model.go` (append ArticleEvent type)
- Create: `backend/internal/repository/event.go`

- [ ] **Step 1: Add ArticleEvent model**

Append to `backend/internal/model/model.go` (after the existing types — find a good spot near other event-like types or at end):

```go
// ArticleEvent records a behavioral signal about a user-article interaction.
// event_type ∈ {"exposure", "click", "completed_read"}.
type ArticleEvent struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int       `json:"user_id" db:"user_id"`
	ArticleID  int       `json:"article_id" db:"article_id"`
	EventType  string    `json:"event_type" db:"event_type"`
	OccurredAt time.Time `json:"occurred_at" db:"occurred_at"`
}

const (
	EventTypeExposure       = "exposure"
	EventTypeClick          = "click"
	EventTypeCompletedRead  = "completed_read"
)
```

- [ ] **Step 2: Create event repository**

Create `backend/internal/repository/event.go`:

```go
package repository

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/model"
)

type EventRepository struct {
	db *sql.DB
}

func NewEventRepository(db *sql.DB) *EventRepository {
	return &EventRepository{db: db}
}

// validEventTypes mirrors the model constants for input validation at the boundary.
var validEventTypes = map[string]bool{
	model.EventTypeExposure:      true,
	model.EventTypeClick:         true,
	model.EventTypeCompletedRead: true,
}

// Insert adds one event row. Caller must validate event_type via IsValidEventType
// before calling — Insert assumes a valid type and lets the DB enforce FKs.
func (r *EventRepository) Insert(userID, articleID int, eventType string) error {
	if !validEventTypes[eventType] {
		return errors.New("invalid event type")
	}
	_, err := r.db.Exec(
		`INSERT INTO article_events (user_id, article_id, event_type) VALUES ($1, $2, $3)`,
		userID, articleID, eventType,
	)
	return err
}

// IsValidEventType exposes the validation map for handlers to early-reject bad input.
func IsValidEventType(t string) bool {
	return validEventTypes[t]
}
```

- [ ] **Step 3: Build**

```bash
cd backend && go build ./... && cd ..
```
Expected: clean build.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/model/model.go backend/internal/repository/event.go
git commit -m "feat(events): ArticleEvent model + EventRepository"
```

---

## Task 5: POST /api/events handler with tests

**Files:**
- Create: `backend/internal/api/event.go`
- Create: `backend/internal/api/event_test.go`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/event_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Validation-only tests. DB-bound integration deferred to manual verification.

func TestEventPost_BadJSON_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &EventHandler{}
	r := gin.New()
	r.POST("/api/events", h.Create)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/events", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEventPost_InvalidEventType_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &EventHandler{}
	r := gin.New()
	r.POST("/api/events", h.Create)

	body, _ := json.Marshal(map[string]any{"article_id": 1, "event_type": "lol"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestEventPost_MissingArticleID_Returns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &EventHandler{}
	r := gin.New()
	r.POST("/api/events", h.Create)

	body, _ := json.Marshal(map[string]any{"event_type": "click"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd backend && go test ./internal/api/ -run TestEventPost -v 2>&1 | tail -10 && cd ..
```
Expected: FAIL — `EventHandler` undefined.

- [ ] **Step 3: Implement handler**

Create `backend/internal/api/event.go`:

```go
package api

import (
	"net/http"

	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type EventHandler struct {
	repo *repository.EventRepository
}

func NewEventHandler(repo *repository.EventRepository) *EventHandler {
	return &EventHandler{repo: repo}
}

// Create logs a behavioral event for the authenticated user.
// POST /api/events  body: { article_id: int, event_type: "exposure" | "click" }
// Note: completed_read events are written by the backend in ProgressHandler;
// this endpoint only accepts exposure and click from the frontend.
func (h *EventHandler) Create(c *gin.Context) {
	var req struct {
		ArticleID int    `json:"article_id"`
		EventType string `json:"event_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ArticleID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "article_id required"})
		return
	}
	if req.EventType != "exposure" && req.EventType != "click" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "event_type must be exposure or click"})
		return
	}
	userID := getUserID(c)
	if err := h.repo.Insert(userID, req.ArticleID, req.EventType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd backend && go test ./internal/api/ -run TestEventPost -v 2>&1 | tail -15 && cd ..
```
Expected: all three PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/event.go backend/internal/api/event_test.go
git commit -m "feat(api): POST /api/events for exposure/click telemetry"
```

---

## Task 6: Wire EventHandler into main.go + completed_read auto-write

**Files:**
- Modify: `backend/cmd/server/main.go`
- Modify: `backend/internal/api/preference.go` — ProgressHandler.Update writes completed_read
- Modify: `backend/internal/repository/progress.go` — return wasCompleted flag for upsert

- [ ] **Step 1: Update progress.Upsert to report transition**

In `backend/internal/repository/progress.go`, lines 32-43, change the Upsert signature so callers can detect the false→true completion transition. Replace with:

```go
// UpsertResult exposes whether is_completed flipped false→true on this call.
type ProgressUpsertResult struct {
	NewlyCompleted bool
}

func (r *ProgressRepository) Upsert(progress *model.ReadingProgress) (ProgressUpsertResult, error) {
	var prev sql.NullBool
	_ = r.db.QueryRow(
		`SELECT is_completed FROM reading_progress WHERE article_id = $1 AND user_id = $2`,
		progress.ArticleID, progress.UserID,
	).Scan(&prev)

	query := `
		INSERT INTO reading_progress (user_id, article_id, scroll_position, last_read_at, is_completed)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (article_id, user_id) DO UPDATE SET
			scroll_position = GREATEST(reading_progress.scroll_position, EXCLUDED.scroll_position),
			last_read_at = EXCLUDED.last_read_at,
			is_completed = reading_progress.is_completed OR EXCLUDED.is_completed
		RETURNING id, scroll_position, is_completed
	`
	err := r.db.QueryRow(query, progress.UserID, progress.ArticleID, progress.ScrollPosition, progress.LastReadAt, progress.IsCompleted).Scan(&progress.ID, &progress.ScrollPosition, &progress.IsCompleted)
	if err != nil {
		return ProgressUpsertResult{}, err
	}
	wasCompleted := prev.Valid && prev.Bool
	return ProgressUpsertResult{NewlyCompleted: !wasCompleted && progress.IsCompleted}, nil
}
```

- [ ] **Step 2: Update ProgressHandler to write completed_read on transition**

In `backend/internal/api/preference.go` line 233-260 (ProgressHandler.Update), add an event repo dependency and emit completed_read when transition happens:

First, change `ProgressHandler` struct (line 204):
```go
type ProgressHandler struct {
	repo      *repository.ProgressRepository
	eventRepo *repository.EventRepository
}

func NewProgressHandler(repo *repository.ProgressRepository, eventRepo *repository.EventRepository) *ProgressHandler {
	return &ProgressHandler{repo: repo, eventRepo: eventRepo}
}
```

Then change `Update` (line 233):
```go
func (h *ProgressHandler) Update(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("article_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid article_id"})
		return
	}

	var req model.UpdateProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID := getUserID(c)
	progress := &model.ReadingProgress{
		UserID:         userID,
		ArticleID:      articleID,
		ScrollPosition: req.ScrollPosition,
		LastReadAt:     time.Now(),
		IsCompleted:    req.IsCompleted,
	}

	result, err := h.repo.Upsert(progress)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if result.NewlyCompleted && h.eventRepo != nil {
		_ = h.eventRepo.Insert(userID, articleID, model.EventTypeCompletedRead)
	}

	c.JSON(http.StatusOK, progress)
}
```

- [ ] **Step 3: Wire dependencies in main.go**

In `backend/cmd/server/main.go`:

After existing repo declarations (around line 30, after `weeklyDigestRepo := repository.NewWeeklyDigestRepository(db)`), add:
```go
	eventRepo := repository.NewEventRepository(db)
```

Find `progressHandler := api.NewProgressHandler(progressRepo)` and replace with:
```go
	progressHandler := api.NewProgressHandler(progressRepo, eventRepo)
```

Find the handler-creation block (around `bookmarkletHandler := ...`) and add at the end:
```go
	eventHandler := api.NewEventHandler(eventRepo)
```

In the `apiGroup` block, after the `// Progress` routes, add:
```go
		// Behavioral events (exposure/click)
		apiGroup.POST("/events", eventHandler.Create)
```

- [ ] **Step 4: Build**

```bash
cd backend && go build ./... && cd ..
```
Expected: clean build. If any caller of `repo.Upsert` other than ProgressHandler exists, fix call site to use new return type.

- [ ] **Step 5: Verify by running**

```bash
docker-compose up -d --build api && sleep 5 && docker-compose logs api 2>&1 | tail -20
```

Expected: API starts cleanly, no "missing token"/runtime errors visible in tail.

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/server/main.go backend/internal/api/preference.go backend/internal/repository/progress.go
git commit -m "feat(events): write completed_read on first false→true transition + register POST /api/events"
```

---

## Task 7: Feed status & weight repo + API endpoints

**Files:**
- Modify: `backend/internal/repository/feed.go` — add UpdateStatus, UpdateWeight
- Modify: `backend/internal/api/feed.go` — add UpdateStatus, UpdateWeight handlers
- Modify: `backend/cmd/server/main.go` — register routes

- [ ] **Step 1: Add repo methods**

Append to `backend/internal/repository/feed.go` (after existing methods, before file end):

```go
// UpdateStatus changes a feed's lifecycle state. Mirrors to is_active for
// backward compat with existing queries: status='active' ↔ is_active=true,
// paused/archived ↔ is_active=false. The is_active column will be dropped
// after Phase 2 once all callers migrate.
func (r *FeedRepository) UpdateStatus(id int, status string) error {
	if status != "active" && status != "paused" && status != "archived" {
		return fmt.Errorf("invalid status: %s", status)
	}
	isActive := status == "active"
	_, err := r.db.Exec(
		`UPDATE feeds SET status = $1, is_active = $2 WHERE id = $3`,
		status, isActive, id,
	)
	return err
}

// UpdateWeight changes a feed's priority weight. Phase 1 stores only;
// Phase 2 verdict scoring multiplies by this value.
func (r *FeedRepository) UpdateWeight(id int, weight float64) error {
	if weight < 0 || weight > 2.0 {
		return fmt.Errorf("priority_weight must be in [0, 2.0]")
	}
	_, err := r.db.Exec(`UPDATE feeds SET priority_weight = $1 WHERE id = $2`, weight, id)
	return err
}
```

- [ ] **Step 2: Migrate GetAllActive to use status**

In `backend/internal/repository/feed.go` line 162-163, replace the query to use status:

```go
func (r *FeedRepository) GetAllActive() ([]model.Feed, error) {
	query := `SELECT id, url, title, last_fetched_at, fetch_interval_minutes, etag, last_modified, is_active, owner_id, feed_type, status, priority_weight, created_at FROM feeds WHERE status = 'active' AND feed_type IN ('rss', 'html', 'youtube', 'podcast')`
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return r.scanFeeds(rows)
}
```

- [ ] **Step 3: Add API handlers**

Append to `backend/internal/api/feed.go`:

```go
// UpdateStatus PATCH /api/feeds/:id/status
func (h *FeedHandler) UpdateStatus(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.UpdateStatus(id, req.Status); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}

// UpdateWeight PATCH /api/feeds/:id/weight
func (h *FeedHandler) UpdateWeight(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req struct {
		PriorityWeight float64 `json:"priority_weight"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.repo.UpdateWeight(id, req.PriorityWeight); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
```

- [ ] **Step 4: Register routes**

In `backend/cmd/server/main.go` Feeds block (around `apiGroup.POST("/feeds/:id/fetch", ...)`), append:

```go
		apiGroup.PATCH("/feeds/:id/status", feedHandler.UpdateStatus)
		apiGroup.PATCH("/feeds/:id/weight", feedHandler.UpdateWeight)
```

- [ ] **Step 5: Build**

```bash
cd backend && go build ./... && cd ..
```
Expected: clean build.

- [ ] **Step 6: Quick smoke test**

```bash
docker-compose up -d --build api && sleep 4 && \
docker-compose logs api 2>&1 | tail -10
```
Expected: API is up, no panic.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/repository/feed.go backend/internal/api/feed.go backend/cmd/server/main.go
git commit -m "feat(feed): UpdateStatus/UpdateWeight repo + PATCH endpoints"
```

---

## Task 8: Feed health service — value score + pruning rules (TDD)

**Files:**
- Create: `backend/internal/service/feed_health.go`
- Create: `backend/internal/service/feed_health_test.go`

- [ ] **Step 1: Write failing tests**

Create `backend/internal/service/feed_health_test.go`:

```go
package service

import (
	"math"
	"testing"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
)

func almostEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestComputeValueScore_Normal(t *testing.T) {
	m := FeedMetrics{
		Exposures:       50,
		Clicks:          20,
		CompletedReads:  10,
		AvgDurationMin:  5.0,
		FeedbackDensity: 2.0,
	}
	got := ComputeValueScore(m)
	// CTR = 0.4, completion = 0.5, normDur = 0.5, normFeedback = 0.4
	// 0.35*0.4 + 0.35*0.5 + 0.20*0.5 + 0.10*0.4 = 0.14 + 0.175 + 0.10 + 0.04 = 0.455
	want := 0.455
	if !almostEqual(got, want, 0.001) {
		t.Errorf("ComputeValueScore = %f, want %f", got, want)
	}
}

func TestComputeValueScore_ColdStartReturnsNaN(t *testing.T) {
	m := FeedMetrics{Exposures: 5}
	got := ComputeValueScore(m)
	if !math.IsNaN(got) {
		t.Errorf("ComputeValueScore for cold start = %f, want NaN", got)
	}
}

func TestComputeValueScore_ZeroClicks(t *testing.T) {
	m := FeedMetrics{Exposures: 50, Clicks: 0}
	// ctr=0, completion=NaN handled→0, dur=0, feedback=0 → 0
	got := ComputeValueScore(m)
	if !almostEqual(got, 0.0, 0.001) {
		t.Errorf("ComputeValueScore zero clicks = %f, want 0", got)
	}
}

func TestPruningRule_FullyDead(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 0,
		ClicksLast90d:   0,
		ProducedLast30d: 0,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R1" {
		t.Errorf("got %+v, want R1", rule)
	}
}

func TestPruningRule_Dormant(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 5,
		ClicksLast90d:   2, // not fully dead
		ProducedLast30d: 5,
		ClicksLast30d:   0, // dormant in 30d
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R2" {
		t.Errorf("got %+v, want R2", rule)
	}
}

func TestPruningRule_DormantBelowMinArticles(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 5,
		ClicksLast90d:   2,
		ProducedLast30d: 1, // below DormantMinArticles=3
		ClicksLast30d:   0,
	}
	// Falls through to R3 since ProducedLast30d=1 (not exactly 0) — actually
	// R3 needs 0. So should return nil (no rule).
	rule := EvaluatePruningRule(m, cfg)
	if rule != nil {
		t.Errorf("got %+v, want nil", rule)
	}
}

func TestPruningRule_DeadFeed(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 1, // not fully dead
		ClicksLast90d:   1,
		ProducedLast30d: 0, // dead source
		ClicksLast30d:   0,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R3" {
		t.Errorf("got %+v, want R3", rule)
	}
}

func TestPruningRule_LowValue(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	score := 0.05
	m := FeedMetrics{
		ProducedLast90d: 30,
		ClicksLast90d:   2,
		ProducedLast30d: 12,
		ClicksLast30d:   1,
		ValueScore:      &score,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R4" {
		t.Errorf("got %+v, want R4", rule)
	}
}

func TestPruningRule_HighVolume(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	score := 0.3
	m := FeedMetrics{
		ProducedLast90d: 300,
		ClicksLast90d:   30,
		ProducedLast30d: 120,
		ClicksLast30d:   25,
		Clicks:          25,
		Exposures:       100,
		CompletedReads:  3, // completion = 0.12 ... wait need <0.05
		ValueScore:      &score,
	}
	// HighVolume needs read_completion = completed/click < 0.05, here 3/25=0.12, not match
	// Adjust: completed=1
	m.CompletedReads = 1
	// 1/25 = 0.04 < 0.05 → match
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R5" {
		t.Errorf("got %+v, want R5", rule)
	}
}

func TestPruningRule_NoMatch(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	score := 0.5
	m := FeedMetrics{
		ProducedLast90d: 30,
		ClicksLast90d:   15,
		ProducedLast30d: 10,
		ClicksLast30d:   8,
		Clicks:          8,
		Exposures:       20,
		CompletedReads:  5,
		ValueScore:      &score,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule != nil {
		t.Errorf("got %+v, want nil (healthy feed)", rule)
	}
}

func TestPruningRule_PriorityFullyDeadOverDormant(t *testing.T) {
	cfg := config.DefaultFeedHealth()
	m := FeedMetrics{
		ProducedLast90d: 0,
		ClicksLast90d:   0,
		ProducedLast30d: 0,
		ClicksLast30d:   0,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R1" {
		t.Errorf("priority should give R1 over R3, got %+v", rule)
	}
}

func TestPruningRule_LastFetchedRecent(t *testing.T) {
	// LastFetchedAt is informational only; rules use article counts.
	cfg := config.DefaultFeedHealth()
	now := time.Now()
	m := FeedMetrics{
		ProducedLast90d: 5,
		ClicksLast90d:   3,
		ProducedLast30d: 0,
		ClicksLast30d:   0,
		LastFetchedAt:   &now,
	}
	rule := EvaluatePruningRule(m, cfg)
	if rule == nil || rule.ID != "R3" {
		t.Errorf("got %+v, want R3", rule)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

```bash
cd backend && go test ./internal/service/ -run "TestComputeValueScore|TestPruningRule" -v 2>&1 | tail -20 && cd ..
```
Expected: FAIL — `FeedMetrics`, `ComputeValueScore`, `EvaluatePruningRule` undefined.

- [ ] **Step 3: Implement service**

Create `backend/internal/service/feed_health.go`:

```go
package service

import (
	"math"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
)

// FeedMetrics is the per-feed aggregation result computed by the repo layer.
// Window-prefixed counts (Last30d, Last90d) carry the wider 90d data needed by
// pruning rules even when the user is viewing the 30d dashboard.
type FeedMetrics struct {
	FeedID    int
	FeedTitle string

	// Window-bound (matches the user-selected window 30d/90d for display)
	Produced       int
	Exposures      int
	Clicks         int
	CompletedReads int
	AvgDurationMin float64
	FeedbackDensity float64
	LastActiveAt   *time.Time

	// Always 30d (for pruning rules)
	ProducedLast30d int
	ClicksLast30d   int
	// Always 90d (for pruning rules)
	ProducedLast90d int
	ClicksLast90d   int

	LastFetchedAt *time.Time

	// ValueScore is nil for cold start (Exposures < ColdStartMinExposures).
	ValueScore *float64
}

// PruningRule is a hint about an unhealthy feed.
type PruningRule struct {
	ID         string // R1..R5
	Label      string // 中文人类标签
	Reason     string // 一句解释，给抽屉直接展示
	SuggestedActions []string // ["归档","暂停","降权"] 中的子集
}

// ComputeValueScore returns NaN for cold-start metrics, else the weighted score.
// Formula: 0.35*ctr + 0.35*completion + 0.20*norm(avg_duration,10min) + 0.10*norm(feedback_density,5)
func ComputeValueScore(m FeedMetrics) float64 {
	if m.Exposures < config.DefaultFeedHealth().ColdStartMinExposures {
		return math.NaN()
	}
	ctr := safeDiv(float64(m.Clicks), float64(m.Exposures))
	completion := safeDiv(float64(m.CompletedReads), float64(m.Clicks))
	normDur := normalize(m.AvgDurationMin, 10.0)
	normFb := normalize(m.FeedbackDensity, 5.0)
	return 0.35*ctr + 0.35*completion + 0.20*normDur + 0.10*normFb
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// normalize clamps x/target to [0, 1]. Negative inputs (possible when
// dislike > like+save) are clamped to 0.
func normalize(x, target float64) float64 {
	if target == 0 {
		return 0
	}
	v := x / target
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// EvaluatePruningRule applies R1-R5 in priority order. Returns nil if healthy.
func EvaluatePruningRule(m FeedMetrics, cfg config.FeedHealthConfig) *PruningRule {
	// R1 完全失效 — highest priority
	if m.ProducedLast90d == 0 && m.ClicksLast90d == 0 {
		return &PruningRule{
			ID:               "R1",
			Label:            "完全失效",
			Reason:           "90 天内无新文章且 0 次点击",
			SuggestedActions: []string{"归档"},
		}
	}
	// R2 沉睡型
	if m.ClicksLast30d == 0 && m.ProducedLast30d >= cfg.DormantMinArticles {
		return &PruningRule{
			ID:               "R2",
			Label:            "沉睡型",
			Reason:           "30 天内你 0 次点击该 feed（30 天产出 ≥ 3）",
			SuggestedActions: []string{"归档"},
		}
	}
	// R3 死源型
	if m.ProducedLast30d == 0 {
		return &PruningRule{
			ID:               "R3",
			Label:            "死源型",
			Reason:           "30 天内 feed 抓回 0 篇文章",
			SuggestedActions: []string{"暂停", "归档"},
		}
	}
	// R4 低价值
	if m.ValueScore != nil && *m.ValueScore < cfg.LowValueScoreThreshold && m.ProducedLast30d >= cfg.LowValueMinSampleSize {
		return &PruningRule{
			ID:               "R4",
			Label:            "低价值",
			Reason:           "价值得分低于阈值且样本充足",
			SuggestedActions: []string{"归档", "降权"},
		}
	}
	// R5 过水型
	if m.ProducedLast30d > cfg.HighVolumeArticleCount && safeDiv(float64(m.CompletedReads), float64(m.Clicks)) < cfg.HighVolumeMaxCompletionRate {
		return &PruningRule{
			ID:               "R5",
			Label:            "过水型",
			Reason:           "30 天文章 > 100 篇，但完读率 < 5%",
			SuggestedActions: []string{"降权"},
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests, verify they pass**

```bash
cd backend && go test ./internal/service/ -run "TestComputeValueScore|TestPruningRule" -v 2>&1 | tail -25 && cd ..
```
Expected: all 11 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/feed_health.go backend/internal/service/feed_health_test.go
git commit -m "feat(service): feed health value score + 5 pruning rules with tests"
```

---

## Task 9: Feed health repository — windowed metrics SQL

**Files:**
- Create: `backend/internal/repository/feed_health.go`

- [ ] **Step 1: Implement repo**

Create `backend/internal/repository/feed_health.go`:

```go
package repository

import (
	"database/sql"
	"time"

	"github.com/bytedance/rss-pal/internal/service"
)

type FeedHealthRepository struct {
	db *sql.DB
}

func NewFeedHealthRepository(db *sql.DB) *FeedHealthRepository {
	return &FeedHealthRepository{db: db}
}

// ComputeMetrics returns one FeedMetrics per non-archived feed visible to the user.
// `window` is the user-selected window (30d or 90d) for display columns;
// 30d/90d-specific counts (used by pruning rules) are also returned.
//
// Implementation: a single query with three windowed aggregations.
// Archived feeds are excluded; paused feeds are included so user can see why
// they paused them.
func (r *FeedHealthRepository) ComputeMetrics(userID int, window time.Duration) ([]service.FeedMetrics, error) {
	windowSeconds := int(window.Seconds())

	query := `
WITH events AS (
    SELECT article_id, event_type, occurred_at, user_id
    FROM article_events
    WHERE user_id = $1
),
articles_w AS (
    SELECT id, feed_id, fetched_at FROM articles
    WHERE fetched_at >= NOW() - ($2 || ' seconds')::INTERVAL
),
articles_30d AS (
    SELECT id, feed_id FROM articles WHERE fetched_at >= NOW() - INTERVAL '30 days'
),
articles_90d AS (
    SELECT id, feed_id FROM articles WHERE fetched_at >= NOW() - INTERVAL '90 days'
),
prefs_w AS (
    SELECT article_id, signal_type, signal_value FROM user_preferences
    WHERE user_id = $1 AND created_at >= NOW() - ($2 || ' seconds')::INTERVAL
),
read_dur AS (
    SELECT a.feed_id, p.signal_value
    FROM user_preferences p
    JOIN articles a ON a.id = p.article_id
    WHERE p.user_id = $1
      AND p.signal_type = 'read_duration'
      AND p.created_at >= NOW() - ($2 || ' seconds')::INTERVAL
)
SELECT
    f.id,
    COALESCE(f.title, f.url) AS feed_title,
    f.last_fetched_at,
    -- window-bound counts
    (SELECT COUNT(*) FROM articles_w aw WHERE aw.feed_id = f.id) AS produced,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles_w aw ON aw.id = e.article_id
       WHERE e.event_type = 'exposure'
         AND aw.feed_id = f.id) AS exposures,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles_w aw ON aw.id = e.article_id
       WHERE e.event_type = 'click'
         AND aw.feed_id = f.id) AS clicks_w,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles_w aw ON aw.id = e.article_id
       WHERE e.event_type = 'completed_read'
         AND aw.feed_id = f.id) AS completed_w,
    COALESCE((SELECT (PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY signal_value)) / 60.0
       FROM read_dur rd WHERE rd.feed_id = f.id), 0) AS avg_duration_min,
    COALESCE((SELECT
        SUM(CASE signal_type WHEN 'like' THEN 1 WHEN 'save' THEN 1 WHEN 'dislike' THEN -1 ELSE 0 END)::FLOAT
        FROM prefs_w p JOIN articles a ON a.id = p.article_id
        WHERE a.feed_id = f.id), 0) AS feedback_density,
    (SELECT MAX(occurred_at) FROM events e
       JOIN articles a ON a.id = e.article_id
       WHERE e.event_type IN ('click','completed_read')
         AND a.feed_id = f.id) AS last_active_at,
    -- 30d
    (SELECT COUNT(*) FROM articles_30d a30 WHERE a30.feed_id = f.id) AS produced_30d,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles a ON a.id = e.article_id
       WHERE e.event_type = 'click'
         AND e.occurred_at >= NOW() - INTERVAL '30 days'
         AND a.feed_id = f.id) AS clicks_30d,
    -- 90d
    (SELECT COUNT(*) FROM articles_90d a90 WHERE a90.feed_id = f.id) AS produced_90d,
    (SELECT COUNT(DISTINCT e.article_id) FROM events e
       JOIN articles a ON a.id = e.article_id
       WHERE e.event_type = 'click'
         AND e.occurred_at >= NOW() - INTERVAL '90 days'
         AND a.feed_id = f.id) AS clicks_90d
FROM feeds f
WHERE f.status != 'archived'
  AND (f.owner_id IS NULL OR f.owner_id = $1)
ORDER BY f.id
	`

	rows, err := r.db.Query(query, userID, windowSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []service.FeedMetrics
	for rows.Next() {
		var m service.FeedMetrics
		var lastFetched, lastActive sql.NullTime
		err := rows.Scan(
			&m.FeedID, &m.FeedTitle, &lastFetched,
			&m.Produced, &m.Exposures, &m.Clicks, &m.CompletedReads,
			&m.AvgDurationMin, &m.FeedbackDensity, &lastActive,
			&m.ProducedLast30d, &m.ClicksLast30d,
			&m.ProducedLast90d, &m.ClicksLast90d,
		)
		if err != nil {
			return nil, err
		}
		if lastFetched.Valid {
			t := lastFetched.Time
			m.LastFetchedAt = &t
		}
		if lastActive.Valid {
			t := lastActive.Time
			m.LastActiveAt = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Build to verify**

```bash
cd backend && go build ./... && cd ..
```
Expected: clean build.

- [ ] **Step 3: Smoke-test the query against real DB**

After running the migration (Task 1), run a quick SELECT to ensure the query plan is reasonable:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT COUNT(*) AS feed_count FROM feeds WHERE status != 'archived';
"
```
Expected: numeric count (the migration assigned everyone status correctly).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/repository/feed_health.go
git commit -m "feat(repo): FeedHealthRepository with windowed CTE aggregation"
```

---

## Task 10: GET /api/feeds/health endpoint

**Files:**
- Create: `backend/internal/api/feed_health.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Implement handler**

Create `backend/internal/api/feed_health.go`:

```go
package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/bytedance/rss-pal/internal/config"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/bytedance/rss-pal/internal/service"
	"github.com/gin-gonic/gin"
)

type FeedHealthHandler struct {
	repo     *repository.FeedHealthRepository
	feedRepo *repository.FeedRepository
	cfg      config.FeedHealthConfig
}

func NewFeedHealthHandler(repo *repository.FeedHealthRepository, feedRepo *repository.FeedRepository) *FeedHealthHandler {
	return &FeedHealthHandler{repo: repo, feedRepo: feedRepo, cfg: config.DefaultFeedHealth()}
}

// FeedHealthRow is the JSON-serialized per-feed metric for the dashboard.
type FeedHealthRow struct {
	FeedID          int                  `json:"feed_id"`
	FeedTitle       string               `json:"feed_title"`
	Status          string               `json:"status"`
	PriorityWeight  float64              `json:"priority_weight"`
	Produced        int                  `json:"produced"`
	Exposures       int                  `json:"exposures"`
	Clicks          int                  `json:"clicks"`
	CompletedReads  int                  `json:"completed_reads"`
	CTR             *float64             `json:"ctr"`              // null if exposures==0
	ReadCompletion  *float64             `json:"read_completion"`  // null if clicks==0
	AvgDurationMin  float64              `json:"avg_duration_min"`
	FeedbackDensity float64              `json:"feedback_density"`
	LastActiveAt    *time.Time           `json:"last_active_at"`
	LastFetchedAt   *time.Time           `json:"last_fetched_at"`
	ValueScore      *float64             `json:"value_score"` // null on cold start
	PruningRule     *service.PruningRule `json:"pruning_rule,omitempty"`
}

type FeedHealthResponse struct {
	Window string          `json:"window"`
	KPI    FeedHealthKPI   `json:"kpi"`
	Rows   []FeedHealthRow `json:"rows"`
}

type FeedHealthKPI struct {
	TotalActive      int `json:"total_active"`
	Healthy          int `json:"healthy"`
	Dormant          int `json:"dormant"`
	CompletedReadsW  int `json:"completed_reads_w"`
}

// Get GET /api/feeds/health?window=30d|90d
func (h *FeedHealthHandler) Get(c *gin.Context) {
	windowParam := c.DefaultQuery("window", "30d")
	var window time.Duration
	switch windowParam {
	case "30d":
		window = 30 * 24 * time.Hour
	case "90d":
		window = 90 * 24 * time.Hour
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "window must be 30d or 90d"})
		return
	}

	userID := getUserID(c)
	metrics, err := h.repo.ComputeMetrics(userID, window)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Need feed status/weight — fetch the user-visible feeds and join in app code.
	feeds, err := h.feedRepo.GetVisibleByUser(userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	feedMeta := make(map[int]struct {
		Status string
		Weight float64
	}, len(feeds))
	for _, f := range feeds {
		feedMeta[f.ID] = struct {
			Status string
			Weight float64
		}{f.Status, f.PriorityWeight}
	}

	rows := make([]FeedHealthRow, 0, len(metrics))
	totalReads := 0
	healthy, dormant := 0, 0

	for _, m := range metrics {
		// compute value score (NaN if cold start)
		score := service.ComputeValueScore(m)
		var valueScorePtr *float64
		if !isNaN(score) {
			s := score
			valueScorePtr = &s
		}
		m.ValueScore = valueScorePtr

		var ctr, completion *float64
		if m.Exposures > 0 {
			v := float64(m.Clicks) / float64(m.Exposures)
			ctr = &v
		}
		if m.Clicks > 0 {
			v := float64(m.CompletedReads) / float64(m.Clicks)
			completion = &v
		}

		rule := service.EvaluatePruningRule(m, h.cfg)

		meta := feedMeta[m.FeedID]
		row := FeedHealthRow{
			FeedID:          m.FeedID,
			FeedTitle:       m.FeedTitle,
			Status:          meta.Status,
			PriorityWeight:  meta.Weight,
			Produced:        m.Produced,
			Exposures:       m.Exposures,
			Clicks:          m.Clicks,
			CompletedReads:  m.CompletedReads,
			CTR:             ctr,
			ReadCompletion:  completion,
			AvgDurationMin:  m.AvgDurationMin,
			FeedbackDensity: m.FeedbackDensity,
			LastActiveAt:    m.LastActiveAt,
			LastFetchedAt:   m.LastFetchedAt,
			ValueScore:      valueScorePtr,
			PruningRule:     rule,
		}
		rows = append(rows, row)

		totalReads += m.CompletedReads
		if rule == nil && valueScorePtr != nil && *valueScorePtr >= 0.3 {
			healthy++
		}
		if rule != nil && rule.ID == "R2" {
			dormant++
		}
	}

	totalActive := 0
	for _, f := range feeds {
		if f.Status == "active" {
			totalActive++
		}
	}

	c.JSON(http.StatusOK, FeedHealthResponse{
		Window: windowParam,
		KPI: FeedHealthKPI{
			TotalActive:     totalActive,
			Healthy:         healthy,
			Dormant:         dormant,
			CompletedReadsW: totalReads,
		},
		Rows: rows,
	})
	_ = strconv.Itoa(0) // silence unused if any
}

func isNaN(f float64) bool {
	return f != f
}
```

- [ ] **Step 2: Wire in main.go**

In `backend/cmd/server/main.go`:

After `eventRepo := repository.NewEventRepository(db)`, add:
```go
	feedHealthRepo := repository.NewFeedHealthRepository(db)
```

After `eventHandler := api.NewEventHandler(eventRepo)`, add:
```go
	feedHealthHandler := api.NewFeedHealthHandler(feedHealthRepo, feedRepo)
```

In the apiGroup block, near the Feeds routes, append:
```go
		apiGroup.GET("/feeds/health", feedHealthHandler.Get)
```

- [ ] **Step 3: Build**

```bash
cd backend && go build ./... && cd ..
```
Expected: clean build.

- [ ] **Step 4: Quick smoke**

```bash
docker-compose up -d --build api && sleep 5 && \
docker-compose logs api 2>&1 | tail -10
```
Expected: clean startup.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/feed_health.go backend/cmd/server/main.go
git commit -m "feat(api): GET /api/feeds/health with KPI + per-feed metrics + pruning"
```

---

## Task 11: Frontend API client — events / health / feed status

**Files:**
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 1: Add types and helpers**

Append to `frontend/src/api/client.ts` (find a good spot after existing feed helpers, near the bottom):

```ts
// === Feed governance Phase 1 ===

export type EventType = 'exposure' | 'click'

export const postEvent = (articleId: number, eventType: EventType) =>
  api.post('/events', { article_id: articleId, event_type: eventType })

export interface PruningRule {
  id: 'R1' | 'R2' | 'R3' | 'R4' | 'R5'
  label: string
  reason: string
  suggested_actions: string[]
}

export interface FeedHealthRow {
  feed_id: number
  feed_title: string
  status: 'active' | 'paused' | 'archived'
  priority_weight: number
  produced: number
  exposures: number
  clicks: number
  completed_reads: number
  ctr: number | null
  read_completion: number | null
  avg_duration_min: number
  feedback_density: number
  last_active_at: string | null
  last_fetched_at: string | null
  value_score: number | null
  pruning_rule?: PruningRule | null
}

export interface FeedHealthKPI {
  total_active: number
  healthy: number
  dormant: number
  completed_reads_w: number
}

export interface FeedHealthResponse {
  window: '30d' | '90d'
  kpi: FeedHealthKPI
  rows: FeedHealthRow[]
}

export const getFeedHealth = (window: '30d' | '90d' = '30d') =>
  api.get<FeedHealthResponse>(`/feeds/health?window=${window}`).then(r => r.data)

export const updateFeedStatus = (feedId: number, status: 'active' | 'paused' | 'archived') =>
  api.patch(`/feeds/${feedId}/status`, { status })

export const updateFeedWeight = (feedId: number, weight: number) =>
  api.patch(`/feeds/${feedId}/weight`, { priority_weight: weight })
```

- [ ] **Step 2: Verify type-check**

```bash
cd frontend && npx tsc --noEmit && cd ..
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/api/client.ts
git commit -m "feat(api-client): add events + feed health + status helpers"
```

---

## Task 12: useExposureTracking hook (≥10s in viewport)

**Files:**
- Create: `frontend/src/hooks/useExposureTracking.ts`

- [ ] **Step 1: Implement hook**

Create `frontend/src/hooks/useExposureTracking.ts`:

```ts
import { useEffect, useRef } from 'react'
import { postEvent } from '../api/client'

const EXPOSURE_THRESHOLD_MS = 10_000
const VISIBILITY_THRESHOLD = 0.5

// sessionStorage key for already-reported (article_id, event_type) pairs
// to avoid duplicate exposure events within the same browser session.
const SESSION_KEY = 'reportedEvents'

function alreadyReported(articleId: number, type: 'exposure' | 'click'): boolean {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY)
    const set: string[] = raw ? JSON.parse(raw) : []
    return set.includes(`${type}:${articleId}`)
  } catch {
    return false
  }
}

export function markReported(articleId: number, type: 'exposure' | 'click'): void {
  try {
    const raw = sessionStorage.getItem(SESSION_KEY)
    const set: string[] = raw ? JSON.parse(raw) : []
    const key = `${type}:${articleId}`
    if (!set.includes(key)) {
      set.push(key)
      sessionStorage.setItem(SESSION_KEY, JSON.stringify(set))
    }
  } catch {
    // ignore quota errors
  }
}

/**
 * useExposureTracking attaches an IntersectionObserver to a ref'd element
 * and fires a single 'exposure' event after the element has been visibly
 * intersecting (≥0.5) continuously for 10 seconds in this browser session.
 */
export function useExposureTracking(articleId: number) {
  const ref = useRef<HTMLDivElement | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (alreadyReported(articleId, 'exposure')) return

    const observer = new IntersectionObserver(
      entries => {
        const entry = entries[0]
        if (!entry) return
        if (entry.isIntersecting && entry.intersectionRatio >= VISIBILITY_THRESHOLD) {
          if (timerRef.current) return
          timerRef.current = setTimeout(() => {
            markReported(articleId, 'exposure')
            postEvent(articleId, 'exposure').catch(() => {})
            observer.disconnect()
          }, EXPOSURE_THRESHOLD_MS)
        } else {
          if (timerRef.current) {
            clearTimeout(timerRef.current)
            timerRef.current = null
          }
        }
      },
      { threshold: [0, VISIBILITY_THRESHOLD, 1] }
    )

    observer.observe(el)
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current)
        timerRef.current = null
      }
      observer.disconnect()
    }
  }, [articleId])

  return ref
}

/** Fires a click event before navigation; idempotent per session. */
export function reportClick(articleId: number): void {
  if (alreadyReported(articleId, 'click')) return
  markReported(articleId, 'click')
  postEvent(articleId, 'click').catch(() => {})
}
```

- [ ] **Step 2: Verify type-check**

```bash
cd frontend && npx tsc --noEmit && cd ..
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add frontend/src/hooks/useExposureTracking.ts
git commit -m "feat(frontend): useExposureTracking hook + reportClick"
```

---

## Task 13: Wire exposure + click in ArticleListPage

**Files:**
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 1: Import hook + wire to article cards**

Find the article-list row component or inline render in `frontend/src/pages/ArticleListPage.tsx`. The list renders each article inside some clickable wrapper. Each wrapper needs:
1. A ref from `useExposureTracking(article.id)` for the IntersectionObserver
2. An `onClick` (or `onMouseDown` if Link) that calls `reportClick(article.id)` before navigation

Add to imports:
```ts
import { useExposureTracking, reportClick } from '../hooks/useExposureTracking'
```

For each article row in the JSX render, refactor into a small inline component (or new file `ArticleRow.tsx` if file is large) that holds its own hook call. If keeping inline, extract into a sub-component within the same file:

```tsx
function ArticleRow({ article, onClick }: { article: Article; onClick: () => void }) {
  const ref = useExposureTracking(article.id)
  return (
    <div ref={ref} onClick={() => { reportClick(article.id); onClick() }}>
      {/* existing row markup unchanged */}
    </div>
  )
}
```

Then in the list `.map(...)` replace the existing row markup with `<ArticleRow article={a} onClick={...existing onClick logic...} />`.

If existing row uses `<Link>` for navigation, change handler to:
```tsx
<Link to={`/articles/${article.id}`} onClick={() => reportClick(article.id)}>
```

- [ ] **Step 2: Verify type-check**

```bash
cd frontend && npx tsc --noEmit && cd ..
```
Expected: clean.

- [ ] **Step 3: Manual smoke-test in browser**

```bash
docker-compose up -d --build frontend
```
Then open `http://localhost/articles`, scroll to keep an article visible 10s+, then click. Open DevTools Network tab — expect:
- POST `/api/events` with `event_type: "exposure"` after 10s
- POST `/api/events` with `event_type: "click"` on click

- [ ] **Step 4: Verify DB rows**

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT event_type, count(*) FROM article_events
WHERE occurred_at > NOW() - INTERVAL '10 minutes'
GROUP BY event_type;
"
```
Expected: at least one `exposure` and one `click` row.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/ArticleListPage.tsx
git commit -m "feat(frontend): wire exposure (≥10s) + click events in article list"
```

---

## Task 14: ArticlePage — completion stay-time gate

**Files:**
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 1: Add active-time accumulator**

In `frontend/src/pages/ArticlePage.tsx`, find `handleScroll` (around line 179). The change:
1. Add a ref `activeReadSecondsRef` that ticks every second when the page is visible (i.e., `document.visibilityState === 'visible'`)
2. In `handleScroll`, gate `isCompleted` not just on `scrollPosition > 0.9` but also on accumulated active read time meeting the floor.

At the top of the component, add:
```tsx
const activeReadSecondsRef = useRef(0)
useEffect(() => {
  const tick = setInterval(() => {
    if (document.visibilityState === 'visible') {
      activeReadSecondsRef.current += 1
    }
  }, 1000)
  return () => clearInterval(tick)
}, [])
```

In `handleScroll`, change the completion gate (line 190):
```tsx
const minSeconds = Math.min(30, Math.floor((article?.reading_minutes || 1) * 30))
const isCompleted = scrollPosition > 0.9 && activeReadSecondsRef.current >= minSeconds
```

- [ ] **Step 2: Type-check**

```bash
cd frontend && npx tsc --noEmit && cd ..
```
Expected: clean.

- [ ] **Step 3: Manual verify**

```bash
docker-compose up -d --build frontend
```

Open a short article — scroll quickly to bottom in <30s. Refresh. The article should NOT be marked completed (in DB `is_completed=false`).

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT article_id, scroll_position, is_completed
FROM reading_progress
ORDER BY last_read_at DESC LIMIT 5;
"
```

Verify a recent test article shows `scroll_position > 0.9` AND `is_completed = false` if you bailed quickly.

Then leave another article open for 30+ seconds while scrolled to bottom — `is_completed` should flip to `true` and an `article_events` row of type `completed_read` should appear:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT event_type, count(*) FROM article_events
WHERE event_type = 'completed_read'
  AND occurred_at > NOW() - INTERVAL '15 minutes';
"
```
Expected: ≥1.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): completion gate requires min stay-time besides scroll"
```

---

## Task 15: /feeds/health route + page skeleton

**Files:**
- Create: `frontend/src/pages/FeedHealthPage.tsx`
- Modify: `frontend/src/App.tsx` — add route
- Modify: `frontend/src/pages/FeedListPage.tsx` — add nav button

- [ ] **Step 1: Page skeleton**

Create `frontend/src/pages/FeedHealthPage.tsx`:

```tsx
import { useState, useEffect } from 'react'
import { getFeedHealth, FeedHealthResponse } from '../api/client'
import { Link } from 'react-router-dom'

const WINDOW_KEY = 'feedHealthWindow'

export default function FeedHealthPage() {
  const [window, setWindow] = useState<'30d' | '90d'>(() => {
    const saved = localStorage.getItem(WINDOW_KEY)
    return saved === '90d' ? '90d' : '30d'
  })
  const [data, setData] = useState<FeedHealthResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    setLoading(true)
    setError('')
    getFeedHealth(window)
      .then(setData)
      .catch((err) => setError(err?.response?.data?.error || '加载失败'))
      .finally(() => setLoading(false))
    localStorage.setItem(WINDOW_KEY, window)
  }, [window])

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16 }}>
        <h1 style={{ margin: 0 }}>Feed 健康度</h1>
        <Link to="/feeds">← 订阅源管理</Link>
      </div>
      <div style={{ marginBottom: 16 }}>
        时间窗口：
        <button
          onClick={() => setWindow('30d')}
          style={{ marginLeft: 8, fontWeight: window === '30d' ? 'bold' : 'normal' }}
        >30 天</button>
        <button
          onClick={() => setWindow('90d')}
          style={{ marginLeft: 4, fontWeight: window === '90d' ? 'bold' : 'normal' }}
        >90 天</button>
      </div>
      {loading && <div>加载中…</div>}
      {error && <div className="error">{error}</div>}
      {data && (
        <>
          <pre style={{ fontSize: 12, background: '#f5f5f5', padding: 8 }}>
            {JSON.stringify(data, null, 2)}
          </pre>
        </>
      )}
    </div>
  )
}
```
(The `<pre>` is a placeholder — Tasks 16-18 replace it with KPI/table/drawer.)

- [ ] **Step 2: Add route**

In `frontend/src/App.tsx`:

Add import near other page imports:
```tsx
import FeedHealthPage from './pages/FeedHealthPage'
```

Inside `<Route element={<RequireAuth ...>>` block, add:
```tsx
          <Route path="feeds/health" element={<FeedHealthPage />} />
```
(Place it directly after `<Route path="feeds" element={<FeedListPage />} />`.)

- [ ] **Step 3: Add nav button on /feeds**

In `frontend/src/pages/FeedListPage.tsx`, find the page top-level container. After the page header (or near top of returned JSX), add a link:

```tsx
import { Link } from 'react-router-dom'
// ...
<div style={{ marginBottom: 12 }}>
  <Link to="/feeds/health">健康度面板 →</Link>
</div>
```

- [ ] **Step 4: Build & verify**

```bash
cd frontend && npx tsc --noEmit && cd .. && \
docker-compose up -d --build frontend
```

Open `http://localhost/feeds`, click "健康度面板 →", land on `/feeds/health`, see raw JSON.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/pages/FeedHealthPage.tsx frontend/src/App.tsx frontend/src/pages/FeedListPage.tsx
git commit -m "feat(frontend): /feeds/health page skeleton + nav from /feeds"
```

---

## Task 16: KPI cards component

**Files:**
- Create: `frontend/src/components/FeedHealthKPI.tsx`
- Modify: `frontend/src/pages/FeedHealthPage.tsx`

- [ ] **Step 1: KPI component**

Create `frontend/src/components/FeedHealthKPI.tsx`:

```tsx
import { FeedHealthKPI as KPI } from '../api/client'

const cardStyle: React.CSSProperties = {
  flex: 1,
  padding: '12px 16px',
  background: '#fff',
  border: '1px solid #e0e0e0',
  borderRadius: 8,
  textAlign: 'center',
}

const numStyle: React.CSSProperties = {
  fontSize: 28,
  fontWeight: 600,
  lineHeight: '36px',
}

const labelStyle: React.CSSProperties = {
  fontSize: 12,
  color: '#666',
  marginTop: 4,
}

export default function FeedHealthKPI({ kpi, window }: { kpi: KPI; window: '30d' | '90d' }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 16 }}>
      <div style={cardStyle}>
        <div style={numStyle}>{kpi.total_active}</div>
        <div style={labelStyle}>活跃 feed</div>
      </div>
      <div style={cardStyle}>
        <div style={{ ...numStyle, color: '#2a8' }}>{kpi.healthy}</div>
        <div style={labelStyle}>健康</div>
      </div>
      <div style={cardStyle}>
        <div style={{ ...numStyle, color: '#c80' }}>{kpi.dormant}</div>
        <div style={labelStyle}>沉睡</div>
      </div>
      <div style={cardStyle}>
        <div style={numStyle}>{kpi.completed_reads_w}</div>
        <div style={labelStyle}>{window} 完读篇数</div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Wire into FeedHealthPage**

In `frontend/src/pages/FeedHealthPage.tsx`, replace the `<pre>` block:

Add import:
```tsx
import FeedHealthKPI from '../components/FeedHealthKPI'
```

Replace `<pre>...</pre>` with:
```tsx
<FeedHealthKPI kpi={data.kpi} window={data.window} />
{/* table will go here in Task 17 */}
<pre style={{ fontSize: 11 }}>{JSON.stringify(data.rows, null, 2)}</pre>
```

- [ ] **Step 3: Verify**

```bash
cd frontend && npx tsc --noEmit && cd .. && \
docker-compose up -d --build frontend
```

`/feeds/health` shows 4 KPI cards.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/FeedHealthKPI.tsx frontend/src/pages/FeedHealthPage.tsx
git commit -m "feat(frontend): KPI cards on feed health page"
```

---

## Task 17: Sortable health table component

**Files:**
- Create: `frontend/src/components/FeedHealthTable.tsx`
- Modify: `frontend/src/pages/FeedHealthPage.tsx`

- [ ] **Step 1: Implement table**

Create `frontend/src/components/FeedHealthTable.tsx`:

```tsx
import { useState, useMemo } from 'react'
import { FeedHealthRow, updateFeedStatus, updateFeedWeight } from '../api/client'

type SortKey = 'feed_title' | 'produced' | 'ctr' | 'read_completion' | 'avg_duration_min' | 'last_active_at' | 'value_score'

interface Props {
  rows: FeedHealthRow[]
  onChange: () => void
}

const numCell: React.CSSProperties = { textAlign: 'right', padding: '6px 8px', whiteSpace: 'nowrap' }
const labelCell: React.CSSProperties = { padding: '6px 8px' }
const headerStyle: React.CSSProperties = { padding: '8px', borderBottom: '2px solid #ccc', textAlign: 'left', cursor: 'pointer', userSelect: 'none' }

function pct(v: number | null): string {
  if (v == null) return '—'
  return (v * 100).toFixed(1) + '%'
}

function score(v: number | null): string {
  if (v == null) return '样本不足'
  return v.toFixed(2)
}

function relativeTime(iso: string | null): string {
  if (!iso) return '从未'
  const t = new Date(iso).getTime()
  const days = Math.floor((Date.now() - t) / (24 * 3600 * 1000))
  if (days === 0) return '今天'
  if (days === 1) return '昨天'
  if (days < 30) return `${days} 天前`
  if (days < 90) return `${Math.floor(days / 7)} 周前`
  return `${Math.floor(days / 30)} 个月前`
}

export default function FeedHealthTable({ rows, onChange }: Props) {
  const [sortKey, setSortKey] = useState<SortKey>('value_score')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('desc')

  const sorted = useMemo(() => {
    const out = [...rows]
    out.sort((a, b) => {
      const av = a[sortKey] as any
      const bv = b[sortKey] as any
      // null/undefined sort to end regardless of dir
      if (av == null && bv == null) return 0
      if (av == null) return 1
      if (bv == null) return -1
      if (typeof av === 'string') {
        return sortDir === 'asc' ? av.localeCompare(bv) : bv.localeCompare(av)
      }
      return sortDir === 'asc' ? av - bv : bv - av
    })
    return out
  }, [rows, sortKey, sortDir])

  const headerClick = (k: SortKey) => {
    if (sortKey === k) setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
    else { setSortKey(k); setSortDir('desc') }
  }

  const handleStatus = async (feedId: number, status: 'paused' | 'archived') => {
    if (!confirm(`确认${status === 'paused' ? '暂停' : '归档'}该 feed？`)) return
    await updateFeedStatus(feedId, status)
    onChange()
  }

  const handleWeight = async (feedId: number) => {
    const input = prompt('输入新的优先级权重（0.0 - 2.0，默认 1.0，降权常用 0.5）', '0.5')
    if (input == null) return
    const v = parseFloat(input)
    if (isNaN(v) || v < 0 || v > 2) { alert('值必须在 0 到 2 之间'); return }
    await updateFeedWeight(feedId, v)
    onChange()
  }

  return (
    <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
      <thead>
        <tr>
          <th style={headerStyle} onClick={() => headerClick('feed_title')}>Feed</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('produced')}>产出</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('ctr')}>CTR</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('read_completion')}>完读率</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('avg_duration_min')}>平均时长</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('last_active_at')}>最近</th>
          <th style={{ ...headerStyle, textAlign: 'right' }} onClick={() => headerClick('value_score')}>价值得分</th>
          <th style={headerStyle}>权重</th>
          <th style={headerStyle}>动作</th>
          <th style={headerStyle}>⚠</th>
        </tr>
      </thead>
      <tbody>
        {sorted.map(r => (
          <tr key={r.feed_id} style={{ borderBottom: '1px solid #eee' }}>
            <td style={labelCell}>
              {r.feed_title}
              {r.status !== 'active' && (
                <span style={{ marginLeft: 6, fontSize: 11, color: '#888' }}>[{r.status}]</span>
              )}
            </td>
            <td style={numCell}>{r.produced}</td>
            <td style={numCell}>{pct(r.ctr)}</td>
            <td style={numCell}>{pct(r.read_completion)}</td>
            <td style={numCell}>{r.avg_duration_min > 0 ? `${r.avg_duration_min.toFixed(1)} 分` : '—'}</td>
            <td style={numCell}>{relativeTime(r.last_active_at)}</td>
            <td style={numCell}>{score(r.value_score)}</td>
            <td style={numCell}>{r.priority_weight.toFixed(2)}</td>
            <td style={labelCell}>
              <button onClick={() => handleStatus(r.feed_id, 'paused')} disabled={r.status !== 'active'}>暂停</button>
              <button onClick={() => handleStatus(r.feed_id, 'archived')} style={{ marginLeft: 4 }}>归档</button>
              <button onClick={() => handleWeight(r.feed_id)} style={{ marginLeft: 4 }}>降权</button>
            </td>
            <td style={labelCell}>
              {r.pruning_rule && (
                <span title={r.pruning_rule.reason} style={{ color: '#c33', cursor: 'help' }}>
                  ⚠ {r.pruning_rule.label}
                </span>
              )}
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  )
}
```

- [ ] **Step 2: Wire into page**

In `frontend/src/pages/FeedHealthPage.tsx`:

Add import:
```tsx
import FeedHealthTable from '../components/FeedHealthTable'
```

Replace the `<pre>{JSON.stringify(data.rows, ...)}</pre>` line with:
```tsx
<FeedHealthTable rows={data.rows} onChange={() => {
  // refetch
  setLoading(true)
  getFeedHealth(window).then(setData).finally(() => setLoading(false))
}} />
```

- [ ] **Step 3: Type-check + smoke**

```bash
cd frontend && npx tsc --noEmit && cd .. && \
docker-compose up -d --build frontend
```

Open `/feeds/health`. Sortable table appears. Click pause/archive on a test feed, confirm row updates and the feed disappears from `/feeds` (or status badge appears).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/FeedHealthTable.tsx frontend/src/pages/FeedHealthPage.tsx
git commit -m "feat(frontend): sortable feed health table + status/weight actions"
```

---

## Task 18: Pruning suggestion banner + drawer

**Files:**
- Create: `frontend/src/components/PruningDrawer.tsx`
- Modify: `frontend/src/pages/FeedHealthPage.tsx`

- [ ] **Step 1: Drawer component**

Create `frontend/src/components/PruningDrawer.tsx`:

```tsx
import { useState } from 'react'
import { FeedHealthRow, updateFeedStatus, updateFeedWeight } from '../api/client'

interface Props {
  rows: FeedHealthRow[]
  onChange: () => void
}

export default function PruningDrawer({ rows, onChange }: Props) {
  const [open, setOpen] = useState(false)
  const [hidden, setHidden] = useState<Set<number>>(new Set())

  const candidates = rows.filter(r => r.pruning_rule && !hidden.has(r.feed_id))
  if (candidates.length === 0) return null

  const action = async (feedId: number, kind: '归档' | '暂停' | '降权') => {
    if (kind === '归档') await updateFeedStatus(feedId, 'archived')
    else if (kind === '暂停') await updateFeedStatus(feedId, 'paused')
    else await updateFeedWeight(feedId, 0.5)
    onChange()
  }

  const dismiss = (feedId: number) => {
    setHidden(prev => new Set(prev).add(feedId))
  }

  return (
    <div style={{ marginBottom: 16, border: '1px solid #fab', borderRadius: 8, background: '#fff5f5' }}>
      <div
        onClick={() => setOpen(o => !o)}
        style={{ padding: '12px 16px', cursor: 'pointer', display: 'flex', justifyContent: 'space-between' }}
      >
        <strong>⚠ {candidates.length} 个 feed 建议处理</strong>
        <span>{open ? '收起' : '展开'} ▾</span>
      </div>
      {open && (
        <div style={{ padding: '0 16px 12px' }}>
          {candidates.map(r => (
            <div key={r.feed_id} style={{ padding: '10px 0', borderTop: '1px dashed #fab' }}>
              <div>
                <strong>{r.feed_title}</strong>
                <span style={{ marginLeft: 8, padding: '2px 8px', background: '#c33', color: '#fff', borderRadius: 4, fontSize: 11 }}>
                  {r.pruning_rule!.label}
                </span>
              </div>
              <div style={{ fontSize: 13, color: '#666', margin: '4px 0' }}>
                原因：{r.pruning_rule!.reason}
              </div>
              <div>
                {r.pruning_rule!.suggested_actions.map(a => (
                  <button key={a} onClick={() => action(r.feed_id, a as any)} style={{ marginRight: 6 }}>
                    {a}
                  </button>
                ))}
                <button onClick={() => dismiss(r.feed_id)} style={{ marginRight: 6, color: '#888' }}>
                  暂不处理
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 2: Wire into page**

In `frontend/src/pages/FeedHealthPage.tsx`:

Add import:
```tsx
import PruningDrawer from '../components/PruningDrawer'
```

Insert before `<FeedHealthKPI ...>`:
```tsx
<PruningDrawer rows={data.rows} onChange={() => {
  setLoading(true)
  getFeedHealth(window).then(setData).finally(() => setLoading(false))
}} />
```

- [ ] **Step 3: Manual verify**

```bash
docker-compose up -d --build frontend
```

If your real data has any feeds with 0 clicks in 30d, banner appears. Otherwise, temporarily mark a feed via DB to trigger:

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
UPDATE article_events SET occurred_at = NOW() - INTERVAL '60 days'
WHERE event_type = 'click' LIMIT 1;
"
```

(Don't keep that artificial state — undo if needed.)

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/PruningDrawer.tsx frontend/src/pages/FeedHealthPage.tsx
git commit -m "feat(frontend): pruning suggestion drawer with per-feed actions"
```

---

## Task 19: Archived feeds collapsible at bottom of page

**Files:**
- Modify: `frontend/src/pages/FeedHealthPage.tsx`
- Modify: `backend/internal/api/feed_health.go` — include archived in a separate response field

- [ ] **Step 1: Backend — add archived list**

In `backend/internal/api/feed_health.go`, change `FeedHealthResponse`:

```go
type FeedHealthResponse struct {
	Window   string          `json:"window"`
	KPI      FeedHealthKPI   `json:"kpi"`
	Rows     []FeedHealthRow `json:"rows"`
	Archived []ArchivedFeed  `json:"archived"`
}

type ArchivedFeed struct {
	FeedID    int    `json:"feed_id"`
	FeedTitle string `json:"feed_title"`
}
```

In `Get`, before the `c.JSON` response, build the archived list:
```go
	archived := []ArchivedFeed{}
	for _, f := range feeds {
		if f.Status == "archived" {
			archived = append(archived, ArchivedFeed{FeedID: f.ID, FeedTitle: f.Title})
		}
	}
```

Include in response:
```go
	c.JSON(http.StatusOK, FeedHealthResponse{
		Window: windowParam,
		KPI:    FeedHealthKPI{...same as before...},
		Rows:   rows,
		Archived: archived,
	})
```

- [ ] **Step 2: Frontend — add types**

In `frontend/src/api/client.ts`:

```ts
export interface ArchivedFeed {
  feed_id: number
  feed_title: string
}
```

Update `FeedHealthResponse`:
```ts
export interface FeedHealthResponse {
  window: '30d' | '90d'
  kpi: FeedHealthKPI
  rows: FeedHealthRow[]
  archived: ArchivedFeed[]
}
```

- [ ] **Step 3: Frontend — collapsible section**

In `frontend/src/pages/FeedHealthPage.tsx`, after the `<FeedHealthTable ...>`, append:

```tsx
{data.archived.length > 0 && (
  <details style={{ marginTop: 24 }}>
    <summary style={{ cursor: 'pointer', fontSize: 14, color: '#666' }}>
      已归档 ({data.archived.length})
    </summary>
    <ul>
      {data.archived.map(a => (
        <li key={a.feed_id} style={{ padding: '4px 0' }}>
          {a.feed_title}{' '}
          <button
            onClick={async () => {
              await updateFeedStatus(a.feed_id, 'active')
              setLoading(true)
              getFeedHealth(window).then(setData).finally(() => setLoading(false))
            }}
            style={{ fontSize: 12, marginLeft: 8 }}
          >
            恢复
          </button>
        </li>
      ))}
    </ul>
  </details>
)}
```

Add import for `updateFeedStatus`:
```tsx
import { getFeedHealth, FeedHealthResponse, updateFeedStatus } from '../api/client'
```

- [ ] **Step 4: Build & verify**

```bash
cd backend && go build ./... && cd ../frontend && npx tsc --noEmit && cd .. && \
docker-compose up -d --build api frontend
```

If any feeds are archived (try archiving one via Task 17 actions), they appear in collapsed section, click "恢复" returns to active.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/feed_health.go frontend/src/api/client.ts frontend/src/pages/FeedHealthPage.tsx
git commit -m "feat(frontend): archived feeds collapsible with restore action"
```

---

## Task 20: End-to-end verification

**Files:** none

- [ ] **Step 1: Full rebuild from scratch**

```bash
docker-compose down && \
docker-compose up -d --build && \
sleep 8 && \
docker-compose ps
```
Expected: all services up.

- [ ] **Step 2: Verify migration ran**

```bash
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT
  (SELECT COUNT(*) FROM article_events) AS event_rows,
  (SELECT COUNT(*) FROM feeds WHERE status='active') AS active_feeds,
  (SELECT COUNT(*) FROM feeds WHERE status='paused') AS paused_feeds,
  (SELECT COUNT(*) FROM feeds WHERE status='archived') AS archived_feeds;
"
```
Expected: counts make sense (active >= 1; archived = 0 unless you tested).

- [ ] **Step 3: Smoke through UI**

Open `http://localhost/feeds/health`:
- [ ] KPI cards visible with real numbers
- [ ] Table sortable on every column
- [ ] At least one row shows the new `value_score` (numeric or "样本不足")
- [ ] Pause / archive / weight buttons work, refresh updates
- [ ] If any feed has 0 30d-clicks AND ≥3 articles: pruning banner shows R2

- [ ] **Step 4: Smoke through打点**

Open `/articles`. Scroll to keep an article 10s+:

```bash
# wait 15s, then check
sleep 15
docker exec rss-pal-postgres-1 psql -U postgres -d rsspal -c "
SELECT event_type, COUNT(*) FROM article_events
WHERE occurred_at > NOW() - INTERVAL '5 minutes'
GROUP BY event_type;
"
```
Expected: `exposure ≥ 1`. Click an article → re-check, expect `click ≥ 1`. Read for 30+s scrolled to bottom → expect `completed_read ≥ 1`.

- [ ] **Step 5: Commit any cleanup**

```bash
git status --short
```
If anything uncommitted, decide: commit or revert. Then proceed to PR.

---

## Task 21: Push branch & open PR

**Files:** none

- [ ] **Step 1: Push branch**

```bash
git push -u origin feature/feed-governance-phase1
```

- [ ] **Step 2: Open PR**

```bash
gh pr create --title "Phase 1: feed governance — health dashboard + behavioral telemetry" --body "$(cat <<'EOF'
## Summary

Phase 1 of the feed governance roadmap (spec: `docs/superpowers/specs/2026-05-08-feed-governance-design.md`).

- **Behavioral telemetry**: new `article_events` table records `exposure` (≥10s in viewport), `click`, and `completed_read`. Frontend uses `IntersectionObserver` for exposure; backend writes `completed_read` on first false→true progress transition.
- **Completion gate**: `is_completed` no longer flips on bare scroll position — also requires accumulated active stay-time (`min(30s, reading_minutes × 30s)`). Existing 99.8% legacy completion data is left as-is; new metrics use `article_events`.
- **Feed state machine**: `feeds.status` (active / paused / archived) + `feeds.priority_weight` columns. `is_active` is double-written for backward compat.
- **`/feeds/health` dashboard**: 4 KPI cards (active / healthy / dormant / completed-this-window), sortable per-feed metrics table (CTR, completion rate, avg duration, last active, value score), pruning suggestion drawer (5 rule types: R1 完全失效, R2 沉睡, R3 死源, R4 低价值, R5 过水), archived-feeds collapsible.
- **Value score formula**: `0.35×CTR + 0.35×completion + 0.20×norm(avg_duration,10min) + 0.10×norm(feedback_density,5)`. Cold-start (exposures < 10) → null.

## Test plan

- [ ] Migration 015 applies cleanly on existing DB
- [ ] Open `/articles`, keep card visible ≥10s → POST `/api/events` `exposure` row in `article_events`
- [ ] Click an article → POST `/api/events` `click`
- [ ] Read article ≥30s scrolled past 90% → `is_completed=true` AND `completed_read` event written
- [ ] Read article fast (<30s) scrolled past 90% → `is_completed` stays false (gate works)
- [ ] `/feeds/health` page: KPIs render, table sorts, pause/archive/weight actions persist
- [ ] Archive a feed → it disappears from main table and from `/feeds`, appears in "已归档"; click 恢复 brings it back active
- [ ] Pruning drawer surfaces at least one rule on real data (or via manual SQL: backdate clicks to trigger R2)
- [ ] `go test ./...` passes (new tests in `service/feed_health_test.go` and `api/event_test.go`)
- [ ] `npx tsc --noEmit` passes in frontend

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Capture PR URL**

The output of step 2 prints the PR URL. Done.

---

## Self-review checklist (post-write)

Each spec section maps to tasks:

| Spec Section | Task |
|--------------|------|
| 4.1 article_events table | T1 |
| 4.2 reading_progress completion fix | T14 (frontend gate) + T6 (backend completed_read on transition) |
| 4.3 feeds.status/priority_weight columns | T1 + T2 + T7 |
| 5 行为打点修复 | T1, T4, T5, T6 (backend); T11-T14 (frontend) |
| 6 健康度指标 | T8 (formula + tests), T9 (SQL) |
| 7 仪表盘 UI | T15-T19 |
| 8 自动剪荐 | T8 (rules + tests), T18 (drawer) |
| 9 状态管理 | T7 (repo+API), T17 (UI actions) |
| 10 实施顺序 | matches T1-T19 |
| 11 验证标准 | T20 |
| Out-of-scope items | not included (correct) |

Type names cross-reference:
- `FeedMetrics` defined in T8, used in T9 + T10
- `PruningRule` defined in T8, used in T10
- `FeedHealthRow` JSON shape defined in T10 (Go) + T11 (TS) — fields match
- `EventType` defined in T11, used in T12-T14
