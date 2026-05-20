# Article Delete Button (per-user soft hide)

**Date:** 2026-05-20
**Branch:** `feature/article-delete-button`

## Goal

Give the user a way to remove an article from their own view — from every list, search result, and recommendation — without destroying the article row. The action is one-tap with toast-undo, lives under a kebab menu on the detail page, and is fully reversible.

## Non-goals

- Hard delete from `articles`.
- An admin "delete for everyone" action.
- A 回收站 page (not needed yet; undo via toast covers the common case).
- A keyboard shortcut (low-frequency action; can add later if missed).

## Data model

New table `hidden_articles`. Self-contained; nothing else changes.

```sql
-- backend/migrations/026_hidden_articles.sql
CREATE TABLE IF NOT EXISTS hidden_articles (
  id         SERIAL PRIMARY KEY,
  user_id    INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  hidden_at  TIMESTAMP NOT NULL DEFAULT NOW(),
  UNIQUE (user_id, article_id)
);
CREATE INDEX IF NOT EXISTS idx_hidden_articles_user
  ON hidden_articles(user_id);
```

Why a new table:

- Symmetric with `reading_progress` / `article_user_tags` — pure per-user state.
- Trivially undo-able (one DELETE).
- Doesn't collide with `user_preferences` signals (like / dislike / save). Hiding is a *visibility* override, not a feedback signal.

The `articles` row itself is untouched. The worker keeps refreshing it; `(feed_id, url)` dedup still holds. Other users (and share tokens) still see it.

Per the project's "new migrations need manual apply" memory, the migration must be applied by hand:

```bash
docker exec -i rss-pal-postgres-1 psql -U postgres -d rsspal \
  < backend/migrations/026_hidden_articles.sql
```

## API

Two new endpoints, JWT-protected. Both idempotent.

```
POST   /api/articles/:id/hide   →  201 {"hidden": true,  "hidden_at": "<RFC3339>"}
DELETE /api/articles/:id/hide   →  200 {"hidden": false}
```

- `POST` on an already-hidden article returns 200 with the existing `hidden_at`.
- `DELETE` on a not-hidden article returns 200.
- 404 if the article is invisible to the user (feed not owned + not shared).

### `GET /api/articles/:id` response gains one field

```go
type ArticleDetailResponse struct {
    // ... existing fields
    Hidden bool `json:"hidden"` // NEW
}
```

Direct fetch is allowed even when hidden — needed for:
1. Undo toast → page can re-render the article we just hid.
2. Bookmark link to a hidden article (UI shows a banner "已删除 · 恢复").

### Filter rules on existing endpoints

Hidden articles must be excluded *for the current user* from:

| Endpoint | Repo method | Filter style |
|---|---|---|
| `GET /api/articles` | `ArticleRepository.GetAll` | via `buildArticleFilterSQL` |
| `GET /api/articles/grouped` | `GetGroupedByCategory` | explicit LEFT JOIN |
| `GET /api/articles/recommended` | `GetRecommended` | explicit LEFT JOIN |
| `GET /api/articles/search` | `Search` | explicit LEFT JOIN |
| `GET /api/articles/unread-count` | `GetUnreadCount` | explicit LEFT JOIN |
| `GET /api/articles/link-set-recommended` | `GetLinkSetRecommendations` | explicit LEFT JOIN |
| `GET /api/clip` | `ClipRepository.ListClipped` | explicit LEFT JOIN (also affects COUNT) |
| `GET /api/articles/:id/children` | `GetChildren` | explicit LEFT JOIN — hidden children disappear from a link-set parent |

The standard filter clause:

```sql
LEFT JOIN hidden_articles h
  ON h.article_id = <article alias>.id AND h.user_id = $<user_id_param>
WHERE h.id IS NULL
```

### Endpoints that do NOT filter (deliberate)

- `GET /api/articles/:id` — direct lookup; carries `hidden` flag.
- `GET /api/share/:token` — public shares aren't affected by a personal hide.
- Worker queries (`GetArticlesWithoutSummary`, `GetMediaArticlesWithoutTranscript`, `FindArticlesNeedingClassification`, etc.) — hidden is a UI concept; the worker keeps doing its job.
- Stats / weekly digest (`GetTopArticlesInRange`) — historical analytics; the user's intent was "get this out of my queue", not "rewrite history". (Trivially changeable later if the user changes their mind.)

## Backend code structure

```
backend/
├── migrations/026_hidden_articles.sql       NEW
├── internal/
│   ├── repository/
│   │   ├── hidden_article.go                NEW  — Hide/Unhide/IsHidden
│   │   ├── article.go                       MODIFIED — buildArticleFilterSQL + other queries
│   │   └── clip.go                          MODIFIED — ListClipped joins
│   └── api/
│       ├── article.go                       MODIFIED — Hide, Unhide handlers; Hidden flag in GetByID
│       └── ...
└── cmd/server/main.go                       MODIFIED — wire repo, register routes
```

`HiddenArticleRepository` interface:

```go
func (r *HiddenArticleRepository) Hide(userID, articleID int) (time.Time, error)
func (r *HiddenArticleRepository) Unhide(userID, articleID int) error
func (r *HiddenArticleRepository) IsHidden(userID, articleID int) (bool, time.Time, error)
```

`Hide` uses `INSERT ... ON CONFLICT (user_id, article_id) DO UPDATE SET hidden_at = hidden_articles.hidden_at RETURNING hidden_at` so the call is idempotent and returns the *original* hide timestamp.

## Frontend

### API client (`frontend/src/api/client.ts`)

```ts
export async function hideArticle(id: number): Promise<{ hidden_at: string }> { ... }
export async function unhideArticle(id: number): Promise<void> { ... }
```

`ArticleDetailResponse` gains `hidden: boolean`.

### Detail page (`ArticlePage.tsx`)

Action row goes from this:

```
[👍 喜欢] [👎 不喜欢] [⭐ 保存] [✓ 标记已读] [📖 阅读模式]
```

to this:

```
[👍 喜欢] [👎 不喜欢] [⭐ 保存] [✓ 标记已读] [📖 阅读模式] [⋯]
                                                            ↓ click
                                                            ┌──────────┐
                                                            │ 🗑 删除   │  (red)
                                                            └──────────┘
```

Component plan:
- New file `frontend/src/components/ArticleActionsMenu.tsx` — small kebab that opens a popover with a `删除` item (and a place to add future low-frequency actions). Closes on outside click / Escape.

Behavior on `🗑 删除`:
1. Capture `nextId` from the existing prev/next nav list (already computed in `ArticlePage`).
2. Optimistically `await hideArticle(article.id)`. On error, toast `删除失败` and stop.
3. Push `article.id` onto a session-scoped `hiddenArticles` sessionStorage list so the article disappears from any in-memory `/articles` page on remount.
4. Fire `window.dispatchEvent(new Event('refresh-unread'))` so the unread badge updates.
5. Show toast: `已删除 [撤销]`, 5s duration.
6. Navigate: `nextId` → `/articles/${nextId}` (with `state.from` preserved); else `handleBack()`.
7. Undo: clicking 撤销 calls `unhideArticle(id)`, removes the id from sessionStorage, and navigates back to that article (`/articles/${id}`).

### Direct-fetch banner

When `GET /api/articles/:id` returns `hidden: true`, render a banner above the title:

```
┌──────────────────────────────────────────────────┐
│ 🗑 这篇文章已删除 · 仍可查看              [恢复]   │
└──────────────────────────────────────────────────┘
```

Clicking 恢复 calls `unhideArticle(id)` and refetches the article (`hidden` becomes false, banner disappears).

### Toast plumbing

The existing `utils/toast.ts` exports a basic toast (info / success / error). It does not currently support action buttons. Add an optional `action?: { label: string; onClick: () => void }` field on the toast options, render it next to the message, and dismiss on click.

Verify before assuming the API — if the existing toast can't be cleanly extended, fall back to `window.confirm`-style undo prompt or a dedicated `<UndoToast>` component. Document whichever path is taken in the implementation commit message.

## Edge cases

| Case | Behavior |
|---|---|
| Hidden article re-appears in feed (worker refetch) | Stays hidden — `hidden_articles` row is by `article_id`, not URL. |
| User hides, then 保存 on the detail page via direct link | Both states coexist; 保存 doesn't auto-unhide. Article still hidden from saved list. |
| Last article in nav list, then hide | `nextId` is null → fall back to `handleBack()`. |
| Hide on a link-set child | Child disappears from its parent's `LinkSetChildren` section. Parent is unaffected. |
| Hide on a link-set parent | Parent disappears from lists; children are reachable only if the user has their direct URLs. |
| Share token target gets hidden by owner | Recipients still see it — `/api/share/:token` doesn't filter by the owner's hide. |

## Testing

Backend (`go test ./...`):
- `hidden_article_test.go`: Hide → IsHidden true; Hide again → same `hidden_at`; Unhide → IsHidden false; Unhide on non-hidden → no error.
- Update `article_test.go` if it asserts row counts that now exclude hidden articles.

Frontend:
- Manual: hide → toast → undo restores; hide last article → goes back to list; hide on `/articles` flows (refresh list → hidden article is gone).

## Rollout

1. Migration applied manually on the user's DB (per project memory).
2. `docker-compose up -d --build api worker frontend` after merge.
3. No data migration needed — the table starts empty.

## Out of scope (future)

- `回收站` page listing hidden articles with bulk unhide.
- Hide-source action ("hide all from this feed").
- Auto-hide after N days of being ignored.
