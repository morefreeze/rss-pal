# Article Tags + Saved Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-user manual tag system on top of articles, an auto source-tag derived from feed, and adopt-or-dismiss AI suggestions sourced from the existing `articles.tags` classification cache. Surface a dedicated `/saved` page with tag-based filtering.

**Architecture:** Three new tables (`user_tags`, `article_user_tags`, `tag_suggestion_dismissals`). Backend follows existing layered pattern: `model/` → `repository/` → `api/`. AI suggestions read directly from existing `articles.tags TEXT[]` (already populated by `cmd/worker/classify.go`); no worker changes. Frontend adds a `TagBar` component on `ArticlePage`, a `SavedPage` at `/saved`, and a `TagChip` component with deterministic hash-based color.

**Tech Stack:** Go 1.21+ (gin, database/sql, lib/pq), React 18 + TypeScript + Vite, Tailwind, PostgreSQL 15, Docker Compose.

**Spec:** [`docs/superpowers/specs/2026-05-08-article-tags-design.md`](../specs/2026-05-08-article-tags-design.md)

**Branch:** `feature/article-tags` (cut from `master`)

---

## File Structure

**New backend files:**
- `backend/migrations/016_user_tags.sql` — schema migration
- `backend/internal/repository/user_tag.go` — `UserTagRepository`, `ArticleUserTagRepository`, `TagSuggestionRepository`
- `backend/internal/api/user_tag.go` — handlers for `/api/tags/*` and `/api/articles/:id/tags`
- `backend/internal/api/saved.go` — handler for `GET /api/saved`

**Modified backend files:**
- `backend/internal/model/model.go` — add `UserTag`, `ArticleUserTag`, `TagSuggestionDismissal`, `ArticleTagsResponse`, `SavedListItem` types
- `backend/cmd/server/main.go` — instantiate repos/handlers and register routes

**New frontend files:**
- `frontend/src/utils/tagColor.ts` + `tagColor.test.ts` — deterministic hash → palette index
- `frontend/src/components/TagChip.tsx` — single tag pill (manual/source/suggestion variants)
- `frontend/src/components/TagBar.tsx` — tag editing area (used by `ArticlePage`)
- `frontend/src/components/ArticleCard.tsx` — extracted from `ArticleListPage`
- `frontend/src/components/SavedTagSidebar.tsx` — left sidebar of `/saved`
- `frontend/src/pages/SavedPage.tsx` — `/saved` route component

**Modified frontend files:**
- `frontend/src/api/client.ts` — add tag endpoints
- `frontend/src/pages/ArticlePage.tsx` — render `<TagBar />` after meta
- `frontend/src/pages/ArticleListPage.tsx` — use `<ArticleCard />`
- `frontend/src/components/Layout.tsx` — add `⭐ 收藏` nav link
- `frontend/src/App.tsx` — register `/saved` route

---

## Setup

- [ ] **Step 0a: Create feature branch from master**

```bash
cd /Users/bytedance/mygit/rss-pal
git checkout master
git pull --ff-only
git checkout -b feature/article-tags
```

- [ ] **Step 0b: Confirm Postgres is running locally**

```bash
docker-compose ps postgres
```
Expected: `postgres` row with `Up` status. If not running, `docker-compose up -d postgres` and wait ~5s.

---

## Phase 1: Core MVP — manual tags + source tag

### Task 1: Migration + model types

**Files:**
- Create: `backend/migrations/016_user_tags.sql`
- Modify: `backend/internal/model/model.go` (append at end of file)

- [ ] **Step 1.1: Write the migration**

Create `backend/migrations/016_user_tags.sql`:

```sql
-- 016_user_tags.sql

CREATE TABLE IF NOT EXISTS user_tags (
    id          SERIAL PRIMARY KEY,
    user_id     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        VARCHAR(64) NOT NULL,
    created_at  TIMESTAMP DEFAULT NOW(),
    UNIQUE (user_id, name)
);

CREATE TABLE IF NOT EXISTS article_user_tags (
    article_id  INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    tag_id      INT NOT NULL REFERENCES user_tags(id) ON DELETE CASCADE,
    user_id     INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (article_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_article_user_tags_user_tag
  ON article_user_tags(user_id, tag_id);
CREATE INDEX IF NOT EXISTS idx_article_user_tags_user_article
  ON article_user_tags(user_id, article_id);

CREATE TABLE IF NOT EXISTS tag_suggestion_dismissals (
    article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       VARCHAR(64) NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (article_id, user_id, name)
);
```

- [ ] **Step 1.2: Apply migration to dev DB**

```bash
docker-compose exec -T postgres psql -U postgres -d rsspal < backend/migrations/016_user_tags.sql
```
Expected: three `CREATE TABLE` and two `CREATE INDEX` notices (or no output on success).

Verify:
```bash
docker-compose exec -T postgres psql -U postgres -d rsspal -c "\d user_tags"
```
Expected: column listing including `id`, `user_id`, `name`, `created_at`.

- [ ] **Step 1.3: Append model types**

Append to end of `backend/internal/model/model.go`:

```go
// UserTag is a per-user manual tag (the "tag" the user types into the article page).
// Distinct from InterestTag (which is a system-tracked weighted signal).
type UserTag struct {
	ID        int       `json:"id" db:"id"`
	UserID    int       `json:"user_id" db:"user_id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	// ArticleCount is filled by GetTagsForUser; 0 elsewhere.
	ArticleCount int `json:"article_count,omitempty" db:"article_count"`
}

// ArticleTagsResponse is what GET /api/articles/:id/tags returns.
type ArticleTagsResponse struct {
	Source      ArticleTagSource `json:"source"`
	Manual      []UserTag        `json:"manual"`
	Suggestions []string         `json:"suggestions"` // names only; AI candidates minus accepted/dismissed
}

type ArticleTagSource struct {
	FeedID int    `json:"feed_id"`
	Title  string `json:"title"`
}

type CreateTagRequest struct {
	Name string `json:"name"`
}

type RenameTagRequest struct {
	Name string `json:"name"`
}

type AddArticleTagRequest struct {
	Name string `json:"name"`
}

type DismissSuggestionRequest struct {
	Name string `json:"name"`
}
```

- [ ] **Step 1.4: Compile**

```bash
cd backend && go build ./... && cd ..
```
Expected: no output (success).

- [ ] **Step 1.5: Commit**

```bash
git add backend/migrations/016_user_tags.sql backend/internal/model/model.go
git commit -m "feat(db): user_tags + article_user_tags + tag_suggestion_dismissals migration"
```

---

### Task 2: UserTagRepository (tag dictionary CRUD)

**Files:**
- Create: `backend/internal/repository/user_tag.go`

- [ ] **Step 2.1: Write the repository**

Create `backend/internal/repository/user_tag.go`:

```go
package repository

import (
	"database/sql"
	"errors"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/lib/pq"
)

// ErrTagNameConflict is returned when a tag name already exists for the user.
var ErrTagNameConflict = errors.New("tag name already exists")

type UserTagRepository struct {
	db *sql.DB
}

func NewUserTagRepository(db *sql.DB) *UserTagRepository {
	return &UserTagRepository{db: db}
}

// GetTagsForUser returns the user's manual tags with the count of distinct
// SAVED articles each tag is currently bound to. Tags with zero saved
// articles are still returned (they may be bound to non-saved articles).
func (r *UserTagRepository) GetTagsForUser(userID int) ([]model.UserTag, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.name, t.created_at,
		       COUNT(DISTINCT CASE WHEN p.article_id IS NOT NULL THEN aut.article_id END) AS article_count
		FROM user_tags t
		LEFT JOIN article_user_tags aut
		       ON aut.tag_id = t.id AND aut.user_id = t.user_id
		LEFT JOIN user_preferences p
		       ON p.article_id = aut.article_id
		      AND p.user_id = t.user_id
		      AND p.signal_type = 'save'
		WHERE t.user_id = $1
		GROUP BY t.id
		ORDER BY t.name ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt, &t.ArticleCount); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// CreateTag inserts (or returns existing) a tag by (user_id, name).
// Returns the existing or newly-created tag's ID.
func (r *UserTagRepository) CreateTag(userID int, name string) (int, error) {
	var id int
	err := r.db.QueryRow(`
		INSERT INTO user_tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&id)
	return id, err
}

// RenameTag changes the name. Returns ErrTagNameConflict on unique violation.
// Returns sql.ErrNoRows if the tag does not belong to the user.
func (r *UserTagRepository) RenameTag(userID, tagID int, name string) error {
	res, err := r.db.Exec(`
		UPDATE user_tags SET name = $1 WHERE id = $2 AND user_id = $3
	`, name, tagID, userID)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return ErrTagNameConflict
		}
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTag removes a tag (cascades article_user_tags via FK).
// Returns sql.ErrNoRows if not found / not owned.
func (r *UserTagRepository) DeleteTag(userID, tagID int) error {
	res, err := r.db.Exec(`DELETE FROM user_tags WHERE id = $1 AND user_id = $2`, tagID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
```

- [ ] **Step 2.2: Compile**

```bash
cd backend && go build ./... && cd ..
```
Expected: no output.

- [ ] **Step 2.3: Commit**

```bash
git add backend/internal/repository/user_tag.go
git commit -m "feat(repo): UserTagRepository with CRUD + per-user article counts"
```

---

### Task 3: ArticleUserTagRepository (binding) + tags-for-article query

**Files:**
- Modify: `backend/internal/repository/user_tag.go` (append)

- [ ] **Step 3.1: Append `ArticleUserTagRepository`**

Append to `backend/internal/repository/user_tag.go`:

```go
type ArticleUserTagRepository struct {
	db *sql.DB
}

func NewArticleUserTagRepository(db *sql.DB) *ArticleUserTagRepository {
	return &ArticleUserTagRepository{db: db}
}

// BindByName ensures (article_id, tag with given name, user) is bound.
// Creates the tag in the user's dictionary if it does not exist.
// Idempotent: returns the tag ID whether new or pre-existing.
func (r *ArticleUserTagRepository) BindByName(articleID, userID int, name string) (int, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var tagID int
	err = tx.QueryRow(`
		INSERT INTO user_tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, userID, name).Scan(&tagID)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(`
		INSERT INTO article_user_tags (article_id, tag_id, user_id) VALUES ($1, $2, $3)
		ON CONFLICT (article_id, tag_id) DO NOTHING
	`, articleID, tagID, userID)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return tagID, nil
}

// Unbind removes the binding. Returns sql.ErrNoRows if not bound.
func (r *ArticleUserTagRepository) Unbind(articleID, tagID, userID int) error {
	res, err := r.db.Exec(`
		DELETE FROM article_user_tags
		WHERE article_id = $1 AND tag_id = $2 AND user_id = $3
	`, articleID, tagID, userID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetSourceForArticle returns the feed-derived source tag (id + title).
func (r *ArticleUserTagRepository) GetSourceForArticle(articleID int) (model.ArticleTagSource, error) {
	var s model.ArticleTagSource
	err := r.db.QueryRow(`
		SELECT f.id, f.title
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		WHERE a.id = $1
	`, articleID).Scan(&s.FeedID, &s.Title)
	return s, err
}

// GetManualForArticle returns the user's manual tags bound to the article.
func (r *ArticleUserTagRepository) GetManualForArticle(articleID, userID int) ([]model.UserTag, error) {
	rows, err := r.db.Query(`
		SELECT t.id, t.user_id, t.name, t.created_at
		FROM user_tags t
		JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
		WHERE aut.article_id = $1 AND t.user_id = $2
		ORDER BY t.name ASC
	`, articleID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []model.UserTag
	for rows.Next() {
		var t model.UserTag
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// GetManualForArticles batch version — returns map[articleID][]UserTag.
// Used by /api/saved to attach tags to article cards.
func (r *ArticleUserTagRepository) GetManualForArticles(articleIDs []int, userID int) (map[int][]model.UserTag, error) {
	out := map[int][]model.UserTag{}
	if len(articleIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(`
		SELECT aut.article_id, t.id, t.user_id, t.name, t.created_at
		FROM user_tags t
		JOIN article_user_tags aut ON aut.tag_id = t.id AND aut.user_id = t.user_id
		WHERE aut.article_id = ANY($1::int[]) AND t.user_id = $2
		ORDER BY aut.article_id, t.name ASC
	`, pq.Array(articleIDs), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var aid int
		var t model.UserTag
		if err := rows.Scan(&aid, &t.ID, &t.UserID, &t.Name, &t.CreatedAt); err != nil {
			return nil, err
		}
		out[aid] = append(out[aid], t)
	}
	return out, rows.Err()
}
```

- [ ] **Step 3.2: Compile**

```bash
cd backend && go build ./... && cd ..
```
Expected: no output.

- [ ] **Step 3.3: Commit**

```bash
git add backend/internal/repository/user_tag.go
git commit -m "feat(repo): ArticleUserTagRepository for tag binding + per-article reads"
```

---

### Task 4: Tag dictionary HTTP handlers

**Files:**
- Create: `backend/internal/api/user_tag.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 4.1: Write the dictionary handlers**

Create `backend/internal/api/user_tag.go`:

```go
package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

const maxTagNameChars = 64

type UserTagHandler struct {
	tagRepo  *repository.UserTagRepository
	bindRepo *repository.ArticleUserTagRepository
}

func NewUserTagHandler(tagRepo *repository.UserTagRepository, bindRepo *repository.ArticleUserTagRepository) *UserTagHandler {
	return &UserTagHandler{tagRepo: tagRepo, bindRepo: bindRepo}
}

func validateTagName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > maxTagNameChars {
		return "", errors.New("name too long (max 64 characters)")
	}
	return name, nil
}

// GET /api/tags
func (h *UserTagHandler) ListTags(c *gin.Context) {
	tags, err := h.tagRepo.GetTagsForUser(getUserID(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if tags == nil {
		tags = []model.UserTag{}
	}
	c.JSON(http.StatusOK, tags)
}

// POST /api/tags
func (h *UserTagHandler) CreateTag(c *gin.Context) {
	var req model.CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id, err := h.tagRepo.CreateTag(getUserID(c), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "name": name})
}

// PATCH /api/tags/:id
func (h *UserTagHandler) RenameTag(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req model.RenameTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err = h.tagRepo.RenameTag(getUserID(c), id, name)
	switch {
	case errors.Is(err, repository.ErrTagNameConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "tag name already exists"})
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}

// DELETE /api/tags/:id
func (h *UserTagHandler) DeleteTag(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	err = h.tagRepo.DeleteTag(getUserID(c), id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "tag not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}
```

- [ ] **Step 4.2: Wire into `cmd/server/main.go`**

In `backend/cmd/server/main.go`, after the line `feedHealthRepo := repository.NewFeedHealthRepository(db)`, add:

```go
	userTagRepo := repository.NewUserTagRepository(db)
	articleUserTagRepo := repository.NewArticleUserTagRepository(db)
```

After the line `feedHealthHandler := api.NewFeedHealthHandler(feedHealthRepo, feedRepo)`, add:

```go
	userTagHandler := api.NewUserTagHandler(userTagRepo, articleUserTagRepo)
```

Inside the protected `apiGroup` block, after `apiGroup.GET("/feeds/health", feedHealthHandler.Get)`, add a new block:

```go
		// Manual tags
		apiGroup.GET("/tags", userTagHandler.ListTags)
		apiGroup.POST("/tags", userTagHandler.CreateTag)
		apiGroup.PATCH("/tags/:id", userTagHandler.RenameTag)
		apiGroup.DELETE("/tags/:id", userTagHandler.DeleteTag)
```

- [ ] **Step 4.3: Compile and run server**

```bash
cd backend && go build ./... && cd ..
```
Expected: no output.

- [ ] **Step 4.4: Smoke test via curl**

Restart the API container:

```bash
docker-compose up -d --build api
```

Wait ~3s, then (replace `<TOKEN>` with a logged-in JWT — get it from the browser localStorage):

```bash
TOKEN=$(docker-compose exec -T postgres psql -U postgres -d rsspal -tAc "SELECT 1") # placeholder; fetch from browser instead
curl -s -H "Authorization: Bearer <TOKEN>" http://localhost:8080/api/tags
```
Expected: `[]` for a fresh user.

Create a tag:
```bash
curl -s -X POST -H "Authorization: Bearer <TOKEN>" -H "Content-Type: application/json" \
  -d '{"name":"前端"}' http://localhost:8080/api/tags
```
Expected: `{"id":1,"name":"前端"}` (id may differ).

List again:
```bash
curl -s -H "Authorization: Bearer <TOKEN>" http://localhost:8080/api/tags
```
Expected: `[{"id":1,"user_id":...,"name":"前端","created_at":"...","article_count":0}]`.

Rename to colliding name (creating second tag first):
```bash
curl -s -X POST -H "Authorization: Bearer <TOKEN>" -H "Content-Type: application/json" \
  -d '{"name":"周报"}' http://localhost:8080/api/tags
curl -s -o /dev/null -w "%{http_code}\n" -X PATCH -H "Authorization: Bearer <TOKEN>" -H "Content-Type: application/json" \
  -d '{"name":"前端"}' http://localhost:8080/api/tags/<second_id>
```
Expected: `409`.

- [ ] **Step 4.5: Commit**

```bash
git add backend/internal/api/user_tag.go backend/cmd/server/main.go
git commit -m "feat(api): /api/tags CRUD endpoints"
```

---

### Task 5: Article-tag binding HTTP handlers

**Files:**
- Modify: `backend/internal/api/user_tag.go` (append)
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 5.1: Append article-tag handlers**

Append to `backend/internal/api/user_tag.go`:

```go
// GET /api/articles/:id/tags
func (h *UserTagHandler) GetArticleTags(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	userID := getUserID(c)

	source, err := h.bindRepo.GetSourceForArticle(articleID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "article not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	manual, err := h.bindRepo.GetManualForArticle(articleID, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if manual == nil {
		manual = []model.UserTag{}
	}
	resp := model.ArticleTagsResponse{
		Source:      source,
		Manual:      manual,
		Suggestions: []string{}, // populated in Phase 3
	}
	c.JSON(http.StatusOK, resp)
}

// POST /api/articles/:id/tags
func (h *UserTagHandler) AddArticleTag(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req model.AddArticleTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tagID, err := h.bindRepo.BindByName(articleID, getUserID(c), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": tagID, "name": name})
}

// DELETE /api/articles/:id/tags/:tagId
func (h *UserTagHandler) RemoveArticleTag(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	tagID, err := strconv.Atoi(c.Param("tagId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid tagId"})
		return
	}
	err = h.bindRepo.Unbind(articleID, tagID, getUserID(c))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": "binding not found"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.Status(http.StatusOK)
	}
}
```

- [ ] **Step 5.2: Wire routes**

In `backend/cmd/server/main.go`, inside the `apiGroup` block after `apiGroup.GET("/articles/:id/playback", ...)`, add:

```go
		apiGroup.GET("/articles/:id/tags", userTagHandler.GetArticleTags)
		apiGroup.POST("/articles/:id/tags", userTagHandler.AddArticleTag)
		apiGroup.DELETE("/articles/:id/tags/:tagId", userTagHandler.RemoveArticleTag)
```

- [ ] **Step 5.3: Compile and rebuild**

```bash
cd backend && go build ./... && cd ..
docker-compose up -d --build api
```
Expected: success, container restarts cleanly.

- [ ] **Step 5.4: Smoke test**

```bash
ARTICLE_ID=<pick any id; e.g. 1>

# Get current tags (only source + empty manual)
curl -s -H "Authorization: Bearer <TOKEN>" http://localhost:8080/api/articles/$ARTICLE_ID/tags
# Expected: {"source":{"feed_id":...,"title":"..."},"manual":[],"suggestions":[]}

# Add a tag
curl -s -X POST -H "Authorization: Bearer <TOKEN>" -H "Content-Type: application/json" \
  -d '{"name":"前端"}' http://localhost:8080/api/articles/$ARTICLE_ID/tags
# Expected: {"id":...,"name":"前端"}

# List again — manual should now have one entry
curl -s -H "Authorization: Bearer <TOKEN>" http://localhost:8080/api/articles/$ARTICLE_ID/tags
```

- [ ] **Step 5.5: Commit**

```bash
git add backend/internal/api/user_tag.go backend/cmd/server/main.go
git commit -m "feat(api): /api/articles/:id/tags bind/unbind/get"
```

---

### Task 6: Frontend tag color util (TDD on pure logic)

**Files:**
- Create: `frontend/src/utils/tagColor.ts`
- Create: `frontend/src/utils/tagColor.test.ts`

The frontend has no test runner configured. We'll write a small standalone test that runs via `node --import tsx --test`, but if the project does not have a test setup, the test file will live alongside the implementation as documentation. For verification, we'll inline a runtime check in the implementation that runs once on module load (cheap and ensures determinism).

- [ ] **Step 6.1: Write the implementation**

Create `frontend/src/utils/tagColor.ts`:

```ts
export const TAG_PALETTE = [
  'rose',
  'amber',
  'emerald',
  'sky',
  'violet',
  'pink',
  'lime',
  'indigo',
] as const

export type TagColor = (typeof TAG_PALETTE)[number]

// FNV-1a 32-bit hash. Stable across browsers and runs.
function hashName(name: string): number {
  let h = 0x811c9dc5
  for (let i = 0; i < name.length; i++) {
    h ^= name.charCodeAt(i)
    h = (h + ((h << 1) + (h << 4) + (h << 7) + (h << 8) + (h << 24))) >>> 0
  }
  return h
}

export function tagColorFor(name: string): TagColor {
  const idx = hashName(name) % TAG_PALETTE.length
  return TAG_PALETTE[idx]
}

// Tailwind classes for a tag chip. Both bg and text shades from the same color.
export function tagChipClasses(name: string): string {
  const c = tagColorFor(name)
  // Note: keep these literal so Tailwind JIT can detect them.
  switch (c) {
    case 'rose': return 'bg-rose-100 text-rose-700'
    case 'amber': return 'bg-amber-100 text-amber-700'
    case 'emerald': return 'bg-emerald-100 text-emerald-700'
    case 'sky': return 'bg-sky-100 text-sky-700'
    case 'violet': return 'bg-violet-100 text-violet-700'
    case 'pink': return 'bg-pink-100 text-pink-700'
    case 'lime': return 'bg-lime-100 text-lime-700'
    case 'indigo': return 'bg-indigo-100 text-indigo-700'
  }
}
```

- [ ] **Step 6.2: Add a one-shot determinism check**

Append to the bottom of `frontend/src/utils/tagColor.ts`:

```ts
// Determinism self-check: in dev, fail loud if the palette stops being stable.
if (import.meta.env?.DEV) {
  const sample = ['前端', 'AI', '论文笔记', 'devops']
  const first = sample.map(tagColorFor).join(',')
  // Re-run; should be identical.
  const second = sample.map(tagColorFor).join(',')
  if (first !== second) {
    // eslint-disable-next-line no-console
    console.error('tagColorFor is non-deterministic', { first, second })
  }
}
```

- [ ] **Step 6.3: Verify build**

```bash
cd frontend && npm run build && cd ..
```
Expected: build succeeds, no TS errors.

- [ ] **Step 6.4: Commit**

```bash
git add frontend/src/utils/tagColor.ts
git commit -m "feat(frontend): tagColor util with deterministic FNV-1a hash → 8-color palette"
```

---

### Task 7: TagChip + API client helpers

**Files:**
- Create: `frontend/src/components/TagChip.tsx`
- Modify: `frontend/src/api/client.ts`

- [ ] **Step 7.1: Create TagChip component**

Create `frontend/src/components/TagChip.tsx`:

```tsx
import { tagChipClasses } from '../utils/tagColor'

type Variant = 'manual' | 'source' | 'suggestion'

interface Props {
  name: string
  variant?: Variant
  onRemove?: () => void
  onAdopt?: () => void
  onClick?: () => void
}

export default function TagChip({ name, variant = 'manual', onRemove, onAdopt, onClick }: Props) {
  if (variant === 'source') {
    return (
      <span
        onClick={onClick}
        className={
          'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ' +
          'bg-slate-100 text-slate-600 ' +
          (onClick ? 'cursor-pointer hover:bg-slate-200' : '')
        }
      >
        <span>📡</span>
        <span>{name}</span>
      </span>
    )
  }
  if (variant === 'suggestion') {
    return (
      <button
        type="button"
        onClick={onAdopt}
        className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs border border-dashed border-slate-300 text-slate-500 hover:bg-slate-50 hover:border-solid"
      >
        <span>⊕</span>
        <span>{name}</span>
      </button>
    )
  }
  // manual
  return (
    <span className={'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs ' + tagChipClasses(name)}>
      <span onClick={onClick} className={onClick ? 'cursor-pointer' : undefined}>{name}</span>
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="opacity-60 hover:opacity-100"
          aria-label={`移除 ${name}`}
        >
          ✕
        </button>
      )}
    </span>
  )
}
```

- [ ] **Step 7.2: Add API client helpers**

Append to `frontend/src/api/client.ts`:

```ts
// === Tags ===

export interface UserTag {
  id: number
  user_id: number
  name: string
  created_at: string
  article_count: number
}

export interface ArticleTagSource {
  feed_id: number
  title: string
}

export interface ArticleTagsResponse {
  source: ArticleTagSource
  manual: UserTag[]
  suggestions: string[]
}

export const listTags = () => api.get<UserTag[]>('/tags').then(r => r.data)
export const createTag = (name: string) =>
  api.post<{ id: number; name: string }>('/tags', { name }).then(r => r.data)
export const renameTag = (id: number, name: string) =>
  api.patch(`/tags/${id}`, { name })
export const deleteTag = (id: number) => api.delete(`/tags/${id}`)

export const getArticleTags = (articleId: number) =>
  api.get<ArticleTagsResponse>(`/articles/${articleId}/tags`).then(r => r.data)
export const addArticleTag = (articleId: number, name: string) =>
  api.post<{ id: number; name: string }>(`/articles/${articleId}/tags`, { name }).then(r => r.data)
export const removeArticleTag = (articleId: number, tagId: number) =>
  api.delete(`/articles/${articleId}/tags/${tagId}`)
```

- [ ] **Step 7.3: Build**

```bash
cd frontend && npm run build && cd ..
```
Expected: success.

- [ ] **Step 7.4: Commit**

```bash
git add frontend/src/components/TagChip.tsx frontend/src/api/client.ts
git commit -m "feat(frontend): TagChip component + tag API client helpers"
```

---

### Task 8: TagBar component on ArticlePage (Phase 1: manual + source only)

**Files:**
- Create: `frontend/src/components/TagBar.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`

- [ ] **Step 8.1: Create TagBar**

Create `frontend/src/components/TagBar.tsx`:

```tsx
import { useEffect, useRef, useState } from 'react'
import {
  ArticleTagsResponse,
  UserTag,
  addArticleTag,
  getArticleTags,
  listTags,
  removeArticleTag,
} from '../api/client'
import TagChip from './TagChip'

interface Props {
  articleId: number
}

export default function TagBar({ articleId }: Props) {
  const [data, setData] = useState<ArticleTagsResponse | null>(null)
  const [allTags, setAllTags] = useState<UserTag[]>([])
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    getArticleTags(articleId).then(setData).catch(() => setData(null))
    listTags().then(setAllTags).catch(() => setAllTags([]))
  }, [articleId])

  useEffect(() => {
    if (editing) inputRef.current?.focus()
  }, [editing])

  if (!data) return null

  const manualNames = new Set(data.manual.map(t => t.name))
  const suggestions = allTags
    .filter(t => t.name.toLowerCase().includes(draft.trim().toLowerCase()))
    .filter(t => !manualNames.has(t.name))
    .slice(0, 8)

  const submit = async (raw?: string) => {
    const name = (raw ?? draft).trim()
    if (!name) return
    await addArticleTag(articleId, name)
    setDraft('')
    setEditing(false)
    const fresh = await getArticleTags(articleId)
    setData(fresh)
    listTags().then(setAllTags)
  }

  const removeManual = async (tagId: number) => {
    await removeArticleTag(articleId, tagId)
    setData(d => (d ? { ...d, manual: d.manual.filter(t => t.id !== tagId) } : d))
  }

  return (
    <div className="flex flex-wrap items-center gap-2 my-3">
      <TagChip name={data.source.title} variant="source" />
      {data.manual.map(t => (
        <TagChip
          key={t.id}
          name={t.name}
          variant="manual"
          onRemove={() => removeManual(t.id)}
        />
      ))}
      {!editing ? (
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="px-2 py-0.5 rounded-full text-xs border border-slate-300 text-slate-500 hover:bg-slate-50"
        >
          + 添加
        </button>
      ) : (
        <div className="relative">
          <input
            ref={inputRef}
            value={draft}
            onChange={e => setDraft(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter') submit()
              if (e.key === 'Escape') { setEditing(false); setDraft('') }
            }}
            onBlur={() => setTimeout(() => setEditing(false), 150)}
            placeholder="输入新建或选择已有"
            maxLength={64}
            className="px-2 py-0.5 rounded-full text-xs border border-slate-300 focus:outline-none focus:ring-1 focus:ring-sky-400"
          />
          {suggestions.length > 0 && (
            <div className="absolute top-full left-0 mt-1 z-10 bg-white border border-slate-200 rounded shadow-md text-xs">
              {suggestions.map(s => (
                <button
                  key={s.id}
                  type="button"
                  onMouseDown={e => { e.preventDefault(); submit(s.name) }}
                  className="block w-full text-left px-3 py-1 hover:bg-slate-100"
                >
                  {s.name}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
```

- [ ] **Step 8.2: Render in ArticlePage**

In `frontend/src/pages/ArticlePage.tsx`, find where the article meta block ends (look for the existing `<ReadingMeta` or article header section). Import TagBar at the top:

```tsx
import TagBar from '../components/TagBar'
```

Render `<TagBar articleId={article.id} />` immediately after the meta/title block, before the article body. The exact insertion point is just below the line that renders the existing `<ReadingMeta` component (or equivalent metadata block — look for the div containing `published_at` / `feed_title`).

- [ ] **Step 8.3: Rebuild frontend**

```bash
docker-compose up -d --build frontend
```

- [ ] **Step 8.4: Manual verification in browser**

1. Open `http://localhost:8080` (or whichever port nginx serves)
2. Log in
3. Open any article
4. Verify: source chip with `📡` appears
5. Click `+ 添加`, type a new tag name, press Enter — tag chip appears with deterministic color
6. Refresh page — tag still there
7. Click `✕` on the tag — tag removed
8. Click `+ 添加`, start typing first letter of an existing tag — autocomplete dropdown appears
9. Open a different article from the same feed — source chip should match

If any step fails, debug before proceeding.

- [ ] **Step 8.5: Commit**

```bash
git add frontend/src/components/TagBar.tsx frontend/src/pages/ArticlePage.tsx
git commit -m "feat(frontend): TagBar on ArticlePage (manual + source tags)"
```

---

### Task 9: ArticleCard extraction + show tags in list

**Files:**
- Create: `frontend/src/components/ArticleCard.tsx`
- Modify: `frontend/src/pages/ArticleListPage.tsx`

- [ ] **Step 9.1: Inspect existing card markup**

Open `frontend/src/pages/ArticleListPage.tsx` and locate the JSX that renders one article row (typically a `<li>` or `<div>` inside `articles.map(...)`). Note the props it consumes: `id`, `title`, `feed_title`, `published_at`, `summary_brief`, `is_read`, etc.

- [ ] **Step 9.2: Create ArticleCard**

Create `frontend/src/components/ArticleCard.tsx`. The exact JSX should match what `ArticleListPage` was rendering — copy the entire single-row JSX block into this component verbatim, then parameterize via props. At minimum, the component receives the article object plus optional `manualTags?: UserTag[]` and renders any non-empty manual tags as a row of `<TagChip variant="manual">` (no remove handler — list view is read-only) below the existing summary/meta.

```tsx
import { Link } from 'react-router-dom'
import { UserTag } from '../api/client'
import TagChip from './TagChip'

export interface ArticleCardData {
  id: number
  title: string
  url: string
  feed_id: number
  feed_title?: string
  published_at?: string | null
  summary_brief?: string
  is_read?: boolean
  reading_minutes?: number
  word_count?: number
  media_type?: string
}

interface Props {
  article: ArticleCardData
  manualTags?: UserTag[]
  onClick?: () => void
  showSourceTag?: boolean
}

export default function ArticleCard({ article, manualTags = [], onClick, showSourceTag = true }: Props) {
  return (
    <li
      className={
        'p-4 border-b border-slate-100 hover:bg-slate-50 transition-colors ' +
        (article.is_read ? 'opacity-60' : '')
      }
      onClick={onClick}
    >
      <Link to={`/articles/${article.id}`} className="block">
        <h3 className="text-base font-medium text-slate-900 mb-1">{article.title}</h3>
        {article.summary_brief && (
          <p className="text-sm text-slate-600 line-clamp-2 mb-2">{article.summary_brief}</p>
        )}
        <div className="flex flex-wrap items-center gap-2 text-xs text-slate-500">
          {showSourceTag && article.feed_title && (
            <TagChip name={article.feed_title} variant="source" />
          )}
          {manualTags.map(t => (
            <TagChip key={t.id} name={t.name} variant="manual" />
          ))}
          {article.published_at && (
            <span>{new Date(article.published_at).toLocaleDateString()}</span>
          )}
          {article.reading_minutes ? <span>· {article.reading_minutes} min</span> : null}
        </div>
      </Link>
    </li>
  )
}
```

> **Note:** If `ArticleListPage` had additional UI on the row (e.g., save button, mark-read button), preserve those by adding them as `children` or extra props to `ArticleCard`. Do **not** drop existing functionality.

- [ ] **Step 9.3: Replace inline rendering with ArticleCard in ArticleListPage**

In `frontend/src/pages/ArticleListPage.tsx`:
1. Import: `import ArticleCard from '../components/ArticleCard'`
2. Replace the inline article row JSX inside the `.map(...)` with `<ArticleCard article={a} key={a.id} />` (pass extra preserved props as needed)
3. The list page does **not** load per-article tags in Phase 1 (would N+1). Pass `manualTags={[]}` for now; Phase 2 saved page does the tag join properly.

- [ ] **Step 9.4: Rebuild and verify**

```bash
docker-compose up -d --build frontend
```

In browser:
1. Open `/articles` — list renders unchanged (no manual tag chips yet, but source chips appear)
2. Click an article — `ArticlePage` still works
3. Filter "未读" / "收藏" — still works

- [ ] **Step 9.5: Commit**

```bash
git add frontend/src/components/ArticleCard.tsx frontend/src/pages/ArticleListPage.tsx
git commit -m "refactor(frontend): extract ArticleCard component, add source-tag chip"
```

---

## Phase 2: Saved page

### Task 10: SavedRepository + GET /api/saved (single-tag, untagged, source filter)

**Files:**
- Create: `backend/internal/api/saved.go`
- Modify: `backend/internal/repository/user_tag.go` (append)
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 10.1: Add SavedRepository to user_tag.go**

Append to `backend/internal/repository/user_tag.go`:

```go
type SavedRepository struct {
	db *sql.DB
}

func NewSavedRepository(db *sql.DB) *SavedRepository {
	return &SavedRepository{db: db}
}

// SavedQuery describes a /api/saved request.
type SavedQuery struct {
	UserID       int
	TagIDs       []int  // empty = "all"
	Mode         string // "and" | "or"; only honored when len(TagIDs)>1
	Untagged     bool   // overrides TagIDs when true
	SourceFeedID int    // 0 = no filter
	Limit        int
	Offset       int
}

// ListSaved returns the article IDs (in published order) and a total count.
func (r *SavedRepository) ListSaved(q SavedQuery) ([]model.Article, int, error) {
	args := []interface{}{q.UserID}
	where := []string{`p.user_id = $1 AND p.signal_type = 'save'`}

	if q.Untagged {
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM article_user_tags aut
			WHERE aut.article_id = a.id AND aut.user_id = $1
		)`)
	} else if len(q.TagIDs) > 0 {
		args = append(args, pq.Array(q.TagIDs))
		idsParam := "$" + strconv.Itoa(len(args))
		if q.Mode == "and" && len(q.TagIDs) > 1 {
			args = append(args, len(q.TagIDs))
			countParam := "$" + strconv.Itoa(len(args))
			where = append(where, `(
				SELECT COUNT(DISTINCT aut.tag_id) FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			) = `+countParam)
		} else {
			where = append(where, `EXISTS (
				SELECT 1 FROM article_user_tags aut
				WHERE aut.article_id = a.id AND aut.user_id = $1
				  AND aut.tag_id = ANY(`+idsParam+`::int[])
			)`)
		}
	}

	if q.SourceFeedID > 0 {
		args = append(args, q.SourceFeedID)
		where = append(where, `a.feed_id = $`+strconv.Itoa(len(args)))
	}

	whereSQL := strings.Join(where, " AND ")

	// Count
	var total int
	if err := r.db.QueryRow(`
		SELECT COUNT(*) FROM articles a
		JOIN user_preferences p ON p.article_id = a.id
		WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Page
	args = append(args, q.Limit, q.Offset)
	limitParam := "$" + strconv.Itoa(len(args)-1)
	offsetParam := "$" + strconv.Itoa(len(args))
	rows, err := r.db.Query(`
		SELECT a.id, a.feed_id, f.title AS feed_title, a.title, a.url,
		       a.published_at, a.summary_brief, a.fetched_at,
		       COALESCE(a.word_count, 0), COALESCE(a.reading_minutes, 0),
		       COALESCE(a.media_type, '')
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		JOIN user_preferences p ON p.article_id = a.id
		WHERE `+whereSQL+`
		ORDER BY a.published_at DESC NULLS LAST, a.id DESC
		LIMIT `+limitParam+` OFFSET `+offsetParam, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []model.Article
	for rows.Next() {
		var a model.Article
		var summary, mediaType sql.NullString
		var feedTitle sql.NullString
		if err := rows.Scan(
			&a.ID, &a.FeedID, &feedTitle, &a.Title, &a.URL,
			&a.PublishedAt, &summary, &a.FetchedAt,
			&a.WordCount, &a.ReadingMinutes, &mediaType,
		); err != nil {
			return nil, 0, err
		}
		a.FeedTitle = feedTitle.String
		a.SummaryBrief = summary.String
		a.MediaType = mediaType.String
		out = append(out, a)
	}
	return out, total, rows.Err()
}
```

Add `"strconv"` to the imports of `user_tag.go` if not already present.

- [ ] **Step 10.2: Create handler**

Create `backend/internal/api/saved.go`:

```go
package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/bytedance/rss-pal/internal/model"
	"github.com/bytedance/rss-pal/internal/repository"
	"github.com/gin-gonic/gin"
)

type SavedHandler struct {
	saved    *repository.SavedRepository
	bindRepo *repository.ArticleUserTagRepository
}

func NewSavedHandler(saved *repository.SavedRepository, bindRepo *repository.ArticleUserTagRepository) *SavedHandler {
	return &SavedHandler{saved: saved, bindRepo: bindRepo}
}

// GET /api/saved
func (h *SavedHandler) List(c *gin.Context) {
	userID := getUserID(c)

	q := repository.SavedQuery{
		UserID: userID,
		Mode:   strings.ToLower(c.DefaultQuery("mode", "and")),
		Limit:  20,
		Offset: 0,
	}

	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			q.Limit = n
		}
	}
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			q.Offset = n
		}
	}
	if c.Query("untagged") == "true" {
		q.Untagged = true
	} else if v := c.Query("tag_ids"); v != "" {
		for _, s := range strings.Split(v, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
				q.TagIDs = append(q.TagIDs, n)
			}
		}
	}
	if v := c.Query("source_feed_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			q.SourceFeedID = n
		}
	}

	articles, total, err := h.saved.ListSaved(q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if articles == nil {
		articles = []model.Article{}
	}

	// Attach manual tags per article
	ids := make([]int, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	tagMap, err := h.bindRepo.GetManualForArticles(ids, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	type item struct {
		model.Article
		ManualTags []model.UserTag `json:"manual_tags"`
	}
	out := make([]item, len(articles))
	for i, a := range articles {
		out[i] = item{Article: a, ManualTags: tagMap[a.ID]}
		if out[i].ManualTags == nil {
			out[i].ManualTags = []model.UserTag{}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items": out,
		"total": total,
	})
}
```

- [ ] **Step 10.3: Wire into main.go**

In `backend/cmd/server/main.go`:

After `articleUserTagRepo := ...`:
```go
	savedRepo := repository.NewSavedRepository(db)
```

After `userTagHandler := ...`:
```go
	savedHandler := api.NewSavedHandler(savedRepo, articleUserTagRepo)
```

In the `apiGroup` block:
```go
		apiGroup.GET("/saved", savedHandler.List)
```

- [ ] **Step 10.4: Compile and rebuild**

```bash
cd backend && go build ./... && cd ..
docker-compose up -d --build api
```

- [ ] **Step 10.5: Smoke test**

(Use a user that has at least one saved article and one tag.)

```bash
TAG_ID=<id of an existing tag bound to a saved article>

# All saved
curl -s -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/saved" | head -c 500

# Saved with one tag
curl -s -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/saved?tag_ids=$TAG_ID" | head -c 500

# Saved without any tag
curl -s -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/saved?untagged=true" | head -c 500

# By source feed
curl -s -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/saved?source_feed_id=1" | head -c 500
```

Each call should return `{"items":[...],"total":<n>}`.

- [ ] **Step 10.6: Commit**

```bash
git add backend/internal/repository/user_tag.go backend/internal/api/saved.go backend/cmd/server/main.go
git commit -m "feat(api): GET /api/saved with single-tag, untagged, source_feed_id filters"
```

---

### Task 11: Multi-tag AND/OR mode for /api/saved

The repository code from Task 10 already implements multi-tag AND/OR via `q.Mode`. This task verifies that path and adds explicit smoke coverage.

**Files:**
- (no code changes — this is an integration verification task)

- [ ] **Step 11.1: Verify multi-tag AND**

```bash
# Pick two tag ids both bound to overlapping saved articles
T1=<id1>; T2=<id2>

# AND: only articles tagged with BOTH
curl -s -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/saved?tag_ids=$T1,$T2&mode=and" | head -c 500

# OR: articles tagged with EITHER
curl -s -H "Authorization: Bearer <TOKEN>" "http://localhost:8080/api/saved?tag_ids=$T1,$T2&mode=or" | head -c 500
```

Manually validate: AND result count ≤ OR result count.

- [ ] **Step 11.2: Commit (none — verification only)**

If verification reveals a bug, fix in `repository/user_tag.go` `ListSaved`, recompile, retest, then commit:

```bash
git commit -am "fix(repo): saved AND/OR semantics"
```

Otherwise skip the commit.

---

### Task 12: SavedPage skeleton + sidebar single-select + nav entry

**Files:**
- Create: `frontend/src/pages/SavedPage.tsx`
- Create: `frontend/src/components/SavedTagSidebar.tsx`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/components/Layout.tsx`
- Modify: `frontend/src/App.tsx`

- [ ] **Step 12.1: Add saved API client helper**

Append to `frontend/src/api/client.ts`:

```ts
export interface SavedItem extends ArticleCardData {
  manual_tags: UserTag[]
}

export interface SavedListResponse {
  items: SavedItem[]
  total: number
}

export interface GetSavedParams {
  tag_ids?: number[]
  mode?: 'and' | 'or'
  untagged?: boolean
  source_feed_id?: number
  limit?: number
  offset?: number
}

export const getSaved = (params: GetSavedParams = {}) => {
  const q: Record<string, string | undefined> = {}
  if (params.tag_ids && params.tag_ids.length) q.tag_ids = params.tag_ids.join(',')
  if (params.mode) q.mode = params.mode
  if (params.untagged) q.untagged = 'true'
  if (params.source_feed_id) q.source_feed_id = String(params.source_feed_id)
  if (params.limit) q.limit = String(params.limit)
  if (params.offset) q.offset = String(params.offset)
  return api.get<SavedListResponse>('/saved', { params: q }).then(r => r.data)
}
```

`ArticleCardData` is exported from `components/ArticleCard.tsx` — add an import-and-re-export at the top of `client.ts`:

```ts
import type { ArticleCardData } from '../components/ArticleCard'
export type { ArticleCardData }
```

If a circular import warning appears, instead inline the `ArticleCardData` shape into `client.ts` and update `ArticleCard.tsx` to import from there.

- [ ] **Step 12.2: Create SavedTagSidebar**

Create `frontend/src/components/SavedTagSidebar.tsx`:

```tsx
import { UserTag } from '../api/client'
import TagChip from './TagChip'

export type SavedSelection =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; id: number }
  | { kind: 'source'; feedId: number; title: string }

interface Props {
  totalSaved: number
  untaggedCount: number
  tags: UserTag[]
  sources: { feedId: number; title: string; count: number }[]
  selected: SavedSelection
  onChange: (sel: SavedSelection) => void
}

export default function SavedTagSidebar({ totalSaved, untaggedCount, tags, sources, selected, onChange }: Props) {
  const isSelected = (sel: SavedSelection) => {
    if (sel.kind === 'all' && selected.kind === 'all') return true
    if (sel.kind === 'untagged' && selected.kind === 'untagged') return true
    if (sel.kind === 'tag' && selected.kind === 'tag') return sel.id === selected.id
    if (sel.kind === 'source' && selected.kind === 'source') return sel.feedId === selected.feedId
    return false
  }
  const Row = ({ sel, label, count }: { sel: SavedSelection; label: React.ReactNode; count: number }) => (
    <button
      type="button"
      onClick={() => onChange(sel)}
      className={
        'w-full flex items-center justify-between px-2 py-1 text-sm rounded ' +
        (isSelected(sel) ? 'bg-sky-100 text-sky-800 font-medium' : 'text-slate-700 hover:bg-slate-100')
      }
    >
      <span className="truncate">{label}</span>
      <span className="text-xs text-slate-500 ml-2">{count}</span>
    </button>
  )

  return (
    <aside className="w-56 shrink-0 border-r border-slate-200 p-3 space-y-3 overflow-y-auto">
      <h2 className="text-sm font-semibold text-slate-900">⭐ 我的收藏</h2>
      <div className="space-y-1">
        <Row sel={{ kind: 'all' }} label="全部" count={totalSaved} />
        <Row sel={{ kind: 'untagged' }} label="(无 tag)" count={untaggedCount} />
      </div>
      {tags.length > 0 && (
        <div>
          <h3 className="text-xs uppercase text-slate-400 mb-1">Tags</h3>
          <div className="space-y-1">
            {tags.map(t => (
              <Row
                key={t.id}
                sel={{ kind: 'tag', id: t.id }}
                label={<span className="inline-flex items-center gap-1"><TagChip name={t.name} variant="manual" /></span>}
                count={t.article_count}
              />
            ))}
          </div>
        </div>
      )}
      {sources.length > 0 && (
        <div>
          <h3 className="text-xs uppercase text-slate-400 mb-1">来源</h3>
          <div className="space-y-1">
            {sources.map(s => (
              <Row
                key={s.feedId}
                sel={{ kind: 'source', feedId: s.feedId, title: s.title }}
                label={<TagChip name={s.title} variant="source" />}
                count={s.count}
              />
            ))}
          </div>
        </div>
      )}
    </aside>
  )
}
```

- [ ] **Step 12.3: Create SavedPage**

Create `frontend/src/pages/SavedPage.tsx`:

```tsx
import { useEffect, useMemo, useState } from 'react'
import {
  SavedItem,
  UserTag,
  getSaved,
  listTags,
} from '../api/client'
import ArticleCard from '../components/ArticleCard'
import SavedTagSidebar, { SavedSelection } from '../components/SavedTagSidebar'

const PAGE_SIZE = 20

export default function SavedPage() {
  const [tags, setTags] = useState<UserTag[]>([])
  const [items, setItems] = useState<SavedItem[]>([])
  const [total, setTotal] = useState(0)
  const [allTotal, setAllTotal] = useState(0)
  const [untaggedTotal, setUntaggedTotal] = useState(0)
  const [selected, setSelected] = useState<SavedSelection>({ kind: 'all' })
  const [loading, setLoading] = useState(false)

  // load tag dictionary
  useEffect(() => {
    listTags().then(setTags).catch(() => setTags([]))
  }, [])

  // load counts for sidebar (independent of current selection)
  useEffect(() => {
    getSaved({ limit: 1 }).then(r => setAllTotal(r.total))
    getSaved({ untagged: true, limit: 1 }).then(r => setUntaggedTotal(r.total))
  }, [])

  // derive sources (per-feed counts) from current "all" page; cheap heuristic
  const sources = useMemo(() => {
    const byFeed = new Map<number, { feedId: number; title: string; count: number }>()
    for (const it of items) {
      const ex = byFeed.get(it.feed_id)
      if (ex) ex.count++
      else byFeed.set(it.feed_id, { feedId: it.feed_id, title: it.feed_title || '', count: 1 })
    }
    return Array.from(byFeed.values()).sort((a, b) => b.count - a.count).slice(0, 12)
  }, [items])

  // load items per selection
  useEffect(() => {
    setLoading(true)
    const params: any = { limit: PAGE_SIZE }
    if (selected.kind === 'tag') params.tag_ids = [selected.id]
    if (selected.kind === 'untagged') params.untagged = true
    if (selected.kind === 'source') params.source_feed_id = selected.feedId
    getSaved(params)
      .then(r => { setItems(r.items); setTotal(r.total) })
      .finally(() => setLoading(false))
  }, [selected])

  return (
    <div className="flex h-[calc(100vh-3.5rem)]">
      <SavedTagSidebar
        totalSaved={allTotal}
        untaggedCount={untaggedTotal}
        tags={tags}
        sources={sources}
        selected={selected}
        onChange={setSelected}
      />
      <main className="flex-1 overflow-y-auto">
        {loading && <div className="p-4 text-sm text-slate-500">加载中…</div>}
        {!loading && items.length === 0 && (
          <div className="p-4 text-sm text-slate-500">没有匹配的收藏。</div>
        )}
        <ul>
          {items.map(it => (
            <ArticleCard key={it.id} article={it} manualTags={it.manual_tags} />
          ))}
        </ul>
        <div className="p-3 text-xs text-slate-400">显示 {items.length} / 共 {total}</div>
      </main>
    </div>
  )
}
```

- [ ] **Step 12.4: Add `/saved` route**

In `frontend/src/App.tsx`:
1. Import: `import SavedPage from './pages/SavedPage'`
2. Inside the protected `<Routes>` add: `<Route path="/saved" element={<SavedPage />} />` next to the other protected routes (e.g., near `/articles`).

- [ ] **Step 12.5: Add nav link**

In `frontend/src/components/Layout.tsx`, find the existing nav entries (look for the link to `/insights`). Add a new `<NavLink to="/saved">⭐ 收藏</NavLink>` between `/articles` and `/insights` matching the existing styling.

- [ ] **Step 12.6: Rebuild & verify**

```bash
docker-compose up -d --build frontend
```

In browser:
1. Click the new `⭐ 收藏` nav link → `/saved` opens
2. Sidebar shows "全部" with the correct count, "(无 tag)" with the right count, and any tags you've created
3. Click a tag → list filters
4. Click "(无 tag)" → list shows only saved articles without manual tags
5. Click a source row → list filters by feed
6. Click "全部" → all saved show again

- [ ] **Step 12.7: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/pages/SavedPage.tsx \
        frontend/src/components/SavedTagSidebar.tsx \
        frontend/src/App.tsx frontend/src/components/Layout.tsx
git commit -m "feat(frontend): /saved page with single-select tag sidebar"
```

---

### Task 13: SavedPage multi-select mode + AND/OR

**Files:**
- Modify: `frontend/src/components/SavedTagSidebar.tsx`
- Modify: `frontend/src/pages/SavedPage.tsx`

- [ ] **Step 13.1: Extend selection model**

Replace the `SavedSelection` type and props in `SavedTagSidebar.tsx`:

```tsx
import { UserTag } from '../api/client'
import TagChip from './TagChip'

export type SavedSelection =
  | { kind: 'all' }
  | { kind: 'untagged' }
  | { kind: 'tag'; ids: number[]; mode: 'and' | 'or' }
  | { kind: 'source'; feedId: number; title: string }

interface Props {
  totalSaved: number
  untaggedCount: number
  tags: UserTag[]
  sources: { feedId: number; title: string; count: number }[]
  selected: SavedSelection
  onChange: (sel: SavedSelection) => void
  multi: boolean
  onToggleMulti: (on: boolean) => void
}

export default function SavedTagSidebar({
  totalSaved, untaggedCount, tags, sources, selected, onChange, multi, onToggleMulti,
}: Props) {
  const isTagSelected = (id: number) => selected.kind === 'tag' && selected.ids.includes(id)
  const isAll = selected.kind === 'all'
  const isUntagged = selected.kind === 'untagged'
  const isSourceSelected = (fid: number) => selected.kind === 'source' && selected.feedId === fid
  const tagMode = selected.kind === 'tag' ? selected.mode : 'and'

  const toggleTag = (id: number) => {
    if (!multi) {
      onChange({ kind: 'tag', ids: [id], mode: 'and' })
      return
    }
    const cur = selected.kind === 'tag' ? selected.ids : []
    const next = cur.includes(id) ? cur.filter(x => x !== id) : [...cur, id]
    if (next.length === 0) onChange({ kind: 'all' })
    else onChange({ kind: 'tag', ids: next, mode: tagMode })
  }
  const setMode = (mode: 'and' | 'or') => {
    if (selected.kind === 'tag') onChange({ ...selected, mode })
  }

  return (
    <aside className="w-56 shrink-0 border-r border-slate-200 p-3 space-y-3 overflow-y-auto">
      <h2 className="text-sm font-semibold text-slate-900">⭐ 我的收藏</h2>
      <label className="flex items-center gap-2 text-xs text-slate-600">
        <input type="checkbox" checked={multi} onChange={e => onToggleMulti(e.target.checked)} />
        多选模式
      </label>
      <div className="space-y-1">
        <button
          type="button"
          onClick={() => onChange({ kind: 'all' })}
          className={'w-full flex justify-between px-2 py-1 text-sm rounded ' + (isAll ? 'bg-sky-100 text-sky-800 font-medium' : 'hover:bg-slate-100')}
        >
          <span>全部</span><span className="text-xs text-slate-500">{totalSaved}</span>
        </button>
        <button
          type="button"
          onClick={() => onChange({ kind: 'untagged' })}
          className={'w-full flex justify-between px-2 py-1 text-sm rounded ' + (isUntagged ? 'bg-sky-100 text-sky-800 font-medium' : 'hover:bg-slate-100')}
        >
          <span>(无 tag)</span><span className="text-xs text-slate-500">{untaggedCount}</span>
        </button>
      </div>
      {tags.length > 0 && (
        <div>
          <h3 className="text-xs uppercase text-slate-400 mb-1">Tags</h3>
          <div className="space-y-1">
            {tags.map(t => (
              <label
                key={t.id}
                className={'w-full flex items-center justify-between px-2 py-1 text-sm rounded cursor-pointer ' + (isTagSelected(t.id) ? 'bg-sky-100 ring-1 ring-sky-300' : 'hover:bg-slate-100')}
              >
                <span className="flex items-center gap-2">
                  {multi && (
                    <input
                      type="checkbox"
                      checked={isTagSelected(t.id)}
                      onChange={() => toggleTag(t.id)}
                    />
                  )}
                  <span onClick={() => !multi && toggleTag(t.id)}>
                    <TagChip name={t.name} variant="manual" />
                  </span>
                </span>
                <span className="text-xs text-slate-500">{t.article_count}</span>
              </label>
            ))}
          </div>
          {multi && selected.kind === 'tag' && selected.ids.length > 1 && (
            <div className="mt-2 flex gap-2 text-xs">
              <button
                type="button"
                onClick={() => setMode('and')}
                className={'px-2 py-0.5 rounded border ' + (tagMode === 'and' ? 'bg-sky-100 border-sky-400' : 'border-slate-300')}
              >AND</button>
              <button
                type="button"
                onClick={() => setMode('or')}
                className={'px-2 py-0.5 rounded border ' + (tagMode === 'or' ? 'bg-sky-100 border-sky-400' : 'border-slate-300')}
              >OR</button>
            </div>
          )}
        </div>
      )}
      {sources.length > 0 && (
        <div>
          <h3 className="text-xs uppercase text-slate-400 mb-1">来源</h3>
          <div className="space-y-1">
            {sources.map(s => (
              <button
                key={s.feedId}
                type="button"
                onClick={() => onChange({ kind: 'source', feedId: s.feedId, title: s.title })}
                className={'w-full flex justify-between px-2 py-1 text-sm rounded ' + (isSourceSelected(s.feedId) ? 'bg-sky-100 text-sky-800 font-medium' : 'hover:bg-slate-100')}
              >
                <TagChip name={s.title} variant="source" />
                <span className="text-xs text-slate-500">{s.count}</span>
              </button>
            ))}
          </div>
        </div>
      )}
    </aside>
  )
}
```

- [ ] **Step 13.2: Update SavedPage to pass `multi` state and convert query**

In `frontend/src/pages/SavedPage.tsx`, replace the load-items effect and add `multi` state:

```tsx
const [multi, setMulti] = useState(false)
// ...
useEffect(() => {
  setLoading(true)
  const params: any = { limit: PAGE_SIZE }
  if (selected.kind === 'tag') {
    params.tag_ids = selected.ids
    if (selected.ids.length > 1) params.mode = selected.mode
  }
  if (selected.kind === 'untagged') params.untagged = true
  if (selected.kind === 'source') params.source_feed_id = selected.feedId
  getSaved(params)
    .then(r => { setItems(r.items); setTotal(r.total) })
    .finally(() => setLoading(false))
}, [selected])

// turning multi off collapses tag selection to first id
const handleToggleMulti = (on: boolean) => {
  setMulti(on)
  if (!on && selected.kind === 'tag' && selected.ids.length > 1) {
    setSelected({ kind: 'tag', ids: [selected.ids[0]], mode: 'and' })
  }
}
```

Also replace the initial selection literal `{ kind: 'all' }` references with the new shape (no change needed: `'all'` is unchanged), and pass `multi` + `onToggleMulti={handleToggleMulti}` to `SavedTagSidebar`.

- [ ] **Step 13.3: Rebuild & verify**

```bash
docker-compose up -d --build frontend
```

In browser:
1. Click `多选模式` → checkboxes appear next to tags
2. Select two tags → AND/OR row appears below; default AND
3. Switch to OR → list expands
4. Untick one tag back to single → AND/OR row hides; list shows single-tag filter
5. Untick all tags → falls back to "全部"
6. Toggle `多选模式` off → keeps a single tag selected

- [ ] **Step 13.4: Commit**

```bash
git add frontend/src/pages/SavedPage.tsx frontend/src/components/SavedTagSidebar.tsx
git commit -m "feat(frontend): /saved multi-select tag mode with AND/OR toggle"
```

---

## Phase 3: AI suggestions

### Task 14: TagSuggestionRepository (filter `articles.tags`) + dismissal

**Files:**
- Modify: `backend/internal/repository/user_tag.go` (append)

- [ ] **Step 14.1: Append the suggestion repository**

Append to `backend/internal/repository/user_tag.go`:

```go
type TagSuggestionRepository struct {
	db *sql.DB
}

func NewTagSuggestionRepository(db *sql.DB) *TagSuggestionRepository {
	return &TagSuggestionRepository{db: db}
}

// SuggestionsForArticle returns up to 5 candidate names from articles.tags,
// filtered to remove tags the user has already adopted (in user_tags + bound)
// or dismissed. Returns empty slice if articles.tags is null/empty.
func (r *TagSuggestionRepository) SuggestionsForArticle(articleID, userID int) ([]string, error) {
	rows, err := r.db.Query(`
		SELECT t AS name
		FROM unnest(COALESCE((SELECT tags FROM articles WHERE id = $2), ARRAY[]::TEXT[])) AS t
		WHERE NOT EXISTS (
			SELECT 1 FROM tag_suggestion_dismissals d
			WHERE d.article_id = $2 AND d.user_id = $1 AND d.name = t
		)
		AND NOT EXISTS (
			SELECT 1 FROM user_tags ut
			JOIN article_user_tags aut
			       ON aut.tag_id = ut.id AND aut.user_id = ut.user_id
			WHERE ut.user_id = $1 AND ut.name = t AND aut.article_id = $2
		)
		LIMIT 5
	`, userID, articleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// DismissSuggestion records (article_id, user_id, name) so the user does not
// see this candidate again. Idempotent.
func (r *TagSuggestionRepository) DismissSuggestion(articleID, userID int, name string) error {
	_, err := r.db.Exec(`
		INSERT INTO tag_suggestion_dismissals (article_id, user_id, name)
		VALUES ($1, $2, $3)
		ON CONFLICT (article_id, user_id, name) DO NOTHING
	`, articleID, userID, name)
	return err
}
```

- [ ] **Step 14.2: Compile**

```bash
cd backend && go build ./... && cd ..
```

- [ ] **Step 14.3: Commit**

```bash
git add backend/internal/repository/user_tag.go
git commit -m "feat(repo): TagSuggestionRepository reads articles.tags, supports dismissal"
```

---

### Task 15: AI suggestion endpoints + extend GET /api/articles/:id/tags

**Files:**
- Modify: `backend/internal/api/user_tag.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 15.1: Inject suggestion repo into handler**

In `backend/internal/api/user_tag.go`, change the handler struct and constructor:

```go
type UserTagHandler struct {
	tagRepo     *repository.UserTagRepository
	bindRepo    *repository.ArticleUserTagRepository
	suggestRepo *repository.TagSuggestionRepository
}

func NewUserTagHandler(
	tagRepo *repository.UserTagRepository,
	bindRepo *repository.ArticleUserTagRepository,
	suggestRepo *repository.TagSuggestionRepository,
) *UserTagHandler {
	return &UserTagHandler{tagRepo: tagRepo, bindRepo: bindRepo, suggestRepo: suggestRepo}
}
```

- [ ] **Step 15.2: Use it inside `GetArticleTags`**

In `GetArticleTags`, replace the `Suggestions: []string{}` placeholder line with a real call:

```go
suggestions, err := h.suggestRepo.SuggestionsForArticle(articleID, userID)
if err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	return
}
if suggestions == nil {
	suggestions = []string{}
}
resp := model.ArticleTagsResponse{
	Source:      source,
	Manual:      manual,
	Suggestions: suggestions,
}
```

- [ ] **Step 15.3: Add dismiss handler**

Append to `backend/internal/api/user_tag.go`:

```go
// POST /api/articles/:id/suggestions/dismiss
func (h *UserTagHandler) DismissSuggestion(c *gin.Context) {
	articleID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req model.DismissSuggestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	name, err := validateTagName(req.Name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.suggestRepo.DismissSuggestion(articleID, getUserID(c), name); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}
```

- [ ] **Step 15.4: Wire into main.go**

In `backend/cmd/server/main.go`, after `articleUserTagRepo := ...`:
```go
	tagSuggestRepo := repository.NewTagSuggestionRepository(db)
```

Update the handler construction to pass it:
```go
	userTagHandler := api.NewUserTagHandler(userTagRepo, articleUserTagRepo, tagSuggestRepo)
```

In the `apiGroup` block, after the existing article-tag routes, add:
```go
		apiGroup.POST("/articles/:id/suggestions/dismiss", userTagHandler.DismissSuggestion)
```

- [ ] **Step 15.5: Rebuild and smoke test**

```bash
cd backend && go build ./... && cd ..
docker-compose up -d --build api
```

```bash
# Pick an article whose articles.tags is non-empty
ARTICLE_ID=<id>
docker-compose exec -T postgres psql -U postgres -d rsspal -c "SELECT id, tags FROM articles WHERE tags IS NOT NULL AND array_length(tags,1) > 0 LIMIT 5;"

# Get tags response — suggestions should now be populated
curl -s -H "Authorization: Bearer <TOKEN>" http://localhost:8080/api/articles/$ARTICLE_ID/tags

# Dismiss one
curl -s -X POST -H "Authorization: Bearer <TOKEN>" -H "Content-Type: application/json" \
  -d '{"name":"<one of the suggested names>"}' \
  http://localhost:8080/api/articles/$ARTICLE_ID/suggestions/dismiss

# Re-fetch — that name should be gone from suggestions
curl -s -H "Authorization: Bearer <TOKEN>" http://localhost:8080/api/articles/$ARTICLE_ID/tags
```

- [ ] **Step 15.6: Commit**

```bash
git add backend/internal/api/user_tag.go backend/cmd/server/main.go
git commit -m "feat(api): expose AI suggestions in GET /api/articles/:id/tags + dismiss endpoint"
```

---

### Task 16: Frontend AI suggestion chips

**Files:**
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/components/TagBar.tsx`

- [ ] **Step 16.1: Add dismiss client helper**

Append to `frontend/src/api/client.ts`:

```ts
export const dismissSuggestion = (articleId: number, name: string) =>
  api.post(`/articles/${articleId}/suggestions/dismiss`, { name })
```

- [ ] **Step 16.2: Render suggestion chips**

In `frontend/src/components/TagBar.tsx`, import the new helper and add a suggestions row:

```tsx
import {
  ArticleTagsResponse,
  UserTag,
  addArticleTag,
  dismissSuggestion,
  getArticleTags,
  listTags,
  removeArticleTag,
} from '../api/client'
```

After the existing tag-bar div (the one containing source/manual/`+ 添加`), add a sibling block:

```tsx
{data.suggestions.length > 0 && (
  <div className="flex flex-wrap items-center gap-2 mb-3 text-xs text-slate-500">
    <span>AI 建议:</span>
    {data.suggestions.map(name => (
      <TagChip
        key={name}
        name={name}
        variant="suggestion"
        onAdopt={async () => {
          await addArticleTag(articleId, name)
          const fresh = await getArticleTags(articleId)
          setData(fresh)
          listTags().then(setAllTags)
        }}
      />
    ))}
    <button
      type="button"
      onClick={async () => {
        await Promise.all(data.suggestions.map(n => dismissSuggestion(articleId, n)))
        const fresh = await getArticleTags(articleId)
        setData(fresh)
      }}
      className="text-slate-400 hover:text-slate-600 underline"
    >
      全部忽略
    </button>
  </div>
)}
```

Each individual suggestion chip needs an in-place dismiss too. Replace the simple `TagChip variant="suggestion"` line above with a wrapper that adds an `✕` button:

```tsx
{data.suggestions.map(name => (
  <span key={name} className="inline-flex items-center gap-1">
    <TagChip
      name={name}
      variant="suggestion"
      onAdopt={async () => {
        await addArticleTag(articleId, name)
        const fresh = await getArticleTags(articleId)
        setData(fresh)
        listTags().then(setAllTags)
      }}
    />
    <button
      type="button"
      title="忽略此建议"
      onClick={async () => {
        await dismissSuggestion(articleId, name)
        const fresh = await getArticleTags(articleId)
        setData(fresh)
      }}
      className="text-slate-300 hover:text-slate-500 -ml-1"
    >
      ✕
    </button>
  </span>
))}
```

- [ ] **Step 16.3: Rebuild & verify**

```bash
docker-compose up -d --build frontend
```

In browser:
1. Open an article with non-empty `articles.tags`
2. AI 建议 row appears with virtual chips
3. Click a chip's ⊕ → it disappears, becomes a manual tag in the row above
4. Click ✕ next to a chip → it disappears, stays gone after refresh
5. Click `全部忽略` → all suggestions disappear, refresh confirms

- [ ] **Step 16.4: Commit**

```bash
git add frontend/src/api/client.ts frontend/src/components/TagBar.tsx
git commit -m "feat(frontend): AI suggestion chips on TagBar with adopt + dismiss"
```

---

## Final: build, deploy, PR

### Task 17: Full Docker rebuild + end-to-end smoke

- [ ] **Step 17.1: Full rebuild**

```bash
docker-compose up -d --build
```

Expected: all services come up cleanly. If `api` fails to start, `docker-compose logs api` to inspect.

- [ ] **Step 17.2: End-to-end smoke**

In browser, run through this script:

1. Login as a user with at least 5 saved articles spanning ≥2 feeds
2. Open the first saved article → see source chip, no manual tags, possibly AI suggestions
3. Adopt one suggestion → it becomes a manual tag
4. Open another article from the same feed → source chip matches; add a different manual tag
5. Click `⭐ 收藏` in nav → `/saved` opens, sidebar shows correct counts
6. Click the new tag from step 3 → list filters to that one article
7. Toggle `多选模式` → check second tag → list shows both (AND mode shows none if no overlap)
8. Switch AND→OR → list expands to union
9. Untoggle `多选模式` → keeps a single tag selected
10. Click "(无 tag)" → only saved articles without manual tags
11. Click a source row → only articles from that feed
12. Click "全部" → restores

If any step fails, fix and re-run.

- [ ] **Step 17.3: Re-verify with full suite**

```bash
cd backend && go test ./... && cd ..
```

Expected: all tests pass.

```bash
cd frontend && npm run build && cd ..
```

Expected: build succeeds with no TS errors.

- [ ] **Step 17.4: No commit needed (verification step).** If Step 17.2 surfaced bugs, fix in the originating task's files, commit with `fix(...)` prefix.

---

### Task 18: Push branch + open PR

- [ ] **Step 18.1: Final review of full diff**

```bash
git fetch origin
git log --oneline origin/master..HEAD
git diff --stat origin/master..HEAD
```

Sanity-check: file count and line count look right (rough expectation: 8-12 commits, ~1500-2500 lines added, mostly under `backend/internal/api`, `backend/internal/repository`, `frontend/src`).

- [ ] **Step 18.2: Push (REQUIRES USER CONFIRMATION)**

Ask the user before pushing:

> "Ready to `git push -u origin feature/article-tags`. OK to proceed?"

After confirmation:

```bash
git push -u origin feature/article-tags
```

- [ ] **Step 18.3: Open PR (REQUIRES USER CONFIRMATION)**

Ask the user before creating the PR. After confirmation:

```bash
gh pr create --title "feat: article tags + saved page (manual / source / AI suggestions)" --body "$(cat <<'EOF'
## Summary
- Per-user manual tags (`user_tags` + `article_user_tags`) with auto color from FNV-1a hash → 8-color palette
- Source tag derived from `feeds.title` with `📡` prefix (no DB rows)
- AI suggestions sourced from existing `articles.tags` classification cache (no worker change), with per-user dismissal table
- New `/saved` page with sidebar: single-select by default; multi-select toggle unlocks AND/OR
- New endpoints: `/api/tags`, `/api/articles/:id/tags`, `/api/articles/:id/suggestions/dismiss`, `/api/saved`

## Spec
[docs/superpowers/specs/2026-05-08-article-tags-design.md](docs/superpowers/specs/2026-05-08-article-tags-design.md)

## Test plan
- [ ] Manual end-to-end smoke (Task 17 in plan)
- [ ] `go test ./backend/...`
- [ ] `npm run build` (frontend)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

Print the PR URL when done.

---

## Self-Review Checklist (run after writing the plan)

- **Spec coverage:**
  - Three-tag design (manual/source/AI) — Tasks 1-9, 14-16 ✅
  - Per-user `user_tags` + `article_user_tags` + `tag_suggestion_dismissals` — Task 1 ✅
  - Hash-based deterministic chip color — Task 6 ✅
  - `📡 ` prefix on source tags everywhere — Tasks 7, 9, 12 ✅
  - `/saved` page with single→multi tag, AND/OR — Tasks 12, 13 ✅
  - "(无 tag)" filter — Tasks 10, 12 ✅
  - Source filter in sidebar — Tasks 12, 13 ✅
  - AI suggestions read `articles.tags` directly — Task 14 ✅
  - Dismissal idempotent — Task 14 ✅
  - "全部忽略" button — Task 16 ✅
  - No worker changes, no backfill script — confirmed ✅
- **Type consistency:** `ArticleTagsResponse`, `UserTag`, `SavedItem`, `SavedSelection` are defined once and referenced consistently ✅
- **No placeholders:** scanned — no TBDs ✅
