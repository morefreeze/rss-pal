# Recommendation Feedback Buttons — Design

Date: 2026-05-08
Branch: `feature/article-tags`

## Goal

Add inline 多推 / 少推 feedback buttons to each article in the "为你推荐" panel on `ArticleListPage`. These buttons let the user correct the recommendation strategy: 多推 reinforces the article's topic + tag profile, 少推 hides the article and dampens its topic + tag weights. After 少推, the panel backfills with the next-best article so the panel stays the same size.

## Non-goals

- No undo affordance for 少推. The signal magnitude is small enough (-0.5 topic, -0.25 tag, clamped at 0) that one accidental click is recoverable through normal usage.
- No `?exclude=` query param on the recommended endpoint. The dislike's -10 article score already drops it below the `score > 0` filter, so a plain refetch returns the right next-N articles.
- 多推 does not backfill or remove the row. The user just acknowledged the article; it stays visible with a `✓ 已多推` badge.

## Background

Existing infrastructure already provides most of what's needed:

- `POST /api/preferences/like` records a `like` signal (+5 to article's recommendation score) and calls `applyCachedClassification` which boosts the user's `interest_topics` (+1.0) and `interest_tags` (+0.5) for the article's classified topic/tags.
- `POST /api/preferences/dislike` records a `dislike` signal (-10 to article's score). It does **not** currently touch `interest_topics` or `interest_tags`.
- `GET /api/articles/recommended?limit=N` returns articles ordered by per-article score, filtering `score > 0` and `not completed`. It does not consult `interest_topics`/`interest_tags`.
- `interest_topics` and `interest_tags` weights are read by AI insights generation (`api/insights.go:156`) — they shape the prompts Claude sees as "here's what the user cares about". They are also surfaced in the user-facing tags UI.

Mapping the user's request onto existing primitives:

| User intent | Mechanism |
|---|---|
| 多推 (more like this) | existing `like`: +5 article score, +1.0 topic, +0.5 tag |
| 少推 (less like this) | existing `dislike`: -10 article score; **plus new behavior**: -0.5 topic, -0.25 tag (clamped at 0) |
| 少推后补充 (backfill) | client refetches `/api/articles/recommended?limit=N`; disliked article is excluded by score |

## Architecture

### Backend

#### `repository/preference.go`

Add two new methods that dampen existing topic/tag rows without inserting new ones:

```go
// DampenTopic reduces the weight of an existing interest_topics row, clamped at 0.
// No-op if the (user_id, topic) row does not exist — disliking a topic the user has
// never engaged with should not create a zero-weight entry.
func (r *PreferenceRepository) DampenTopic(userID int, topic string, delta float64) error {
    if topic == "" || delta >= 0 {
        return nil
    }
    _, err := r.db.Exec(`
        UPDATE interest_topics
        SET weight = GREATEST(weight + $3, 0), last_reinforced_at = NOW()
        WHERE user_id = $1 AND topic = $2
    `, userID, topic, delta)
    return err
}

// DampenTag mirrors DampenTopic for interest_tags.
func (r *PreferenceRepository) DampenTag(userID int, tag string, delta float64) error {
    if tag == "" || delta >= 0 {
        return nil
    }
    _, err := r.db.Exec(`
        UPDATE interest_tags
        SET weight = GREATEST(weight + $3, 0), last_reinforced_at = NOW()
        WHERE user_id = $1 AND tag = $2
    `, userID, tag, delta)
    return err
}
```

Notes:
- `GREATEST(weight + delta, 0)` clamps at zero. Repeated dislikes on a topic the user used to care about gradually pull the weight back to 0 but cannot push it negative.
- `delta >= 0` guard means these methods only act on negative deltas — they are not a general-purpose subtract operation. Reinforcement still goes through `UpsertTopic`/`UpsertTag`.
- No INSERT path: a dislike on a topic the user has never engaged with is a silent no-op. (The article is still hidden via the per-article `dislike` score, so the immediate UX is unaffected.)

#### `api/preference.go`

Add a `dampenCachedClassification` helper mirroring `applyCachedClassification`:

```go
// dampenCachedClassification, when the article already has a cached classification,
// reduces the user's topic + tag weights for it. Mirrors applyCachedClassification
// but with a negative magnitude. Silently no-ops when the article is not yet classified.
func (h *PreferenceHandler) dampenCachedClassification(userID, articleID int) {
    if h.articleRepo == nil {
        return
    }
    topic, tags, err := h.articleRepo.GetClassification(articleID)
    if err != nil || topic == "" {
        return
    }
    const dampenStrength = 0.5 // half of like's +1.0
    topicDelta := -SignalToTopicWeight(dampenStrength) // -0.5
    tagDelta := -SignalToTagWeight(dampenStrength)     // -0.25
    _ = h.prefRepo.DampenTopic(userID, topic, topicDelta)
    for _, t := range tags {
        _ = h.prefRepo.DampenTag(userID, t, tagDelta)
    }
}
```

Modify the existing `Dislike` handler to call this helper after the preference row is written:

```go
func (h *PreferenceHandler) Dislike(c *gin.Context) {
    var req model.PreferenceRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
        return
    }

    pref := &model.UserPreference{
        UserID:      getUserID(c),
        ArticleID:   req.ArticleID,
        SignalType:  "dislike",
        SignalValue: 1.0,
    }

    if err := h.prefRepo.Add(pref); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
        return
    }

    h.dampenCachedClassification(pref.UserID, pref.ArticleID)
    c.Status(http.StatusOK)
}
```

The dampen step is best-effort and never bubbles errors — same convention as the existing `applyCachedClassification` calls in `Like` / `Save`.

### Frontend

#### `pages/ArticleListPage.tsx`

The recommended panel JSX currently renders:

```tsx
{showRecommended && recommended.map(article => (
  <Link key={article.id} to={`/articles/${article.id}`} className="rec-row">
    <div className="flex-between">
      <div style={{ flex: 1 }}>...</div>
    </div>
  </Link>
))}
```

**State additions** (alongside existing `recommended` and `showRecommended`):

```tsx
// Article IDs the user has clicked 多推 on this session — UI shows a "✓ 已多推"
// badge in place of the buttons. Resets on page reload (server-side state is durable).
const [boostedIds, setBoostedIds] = useState<Set<number>>(new Set())
```

No `dismissed` state is needed: the optimistic remove for 少推 directly mutates the `recommended` array.

**Handlers:**

```tsx
const handleBoost = async (articleId: number, e: React.MouseEvent) => {
  e.preventDefault()
  e.stopPropagation()
  setBoostedIds(prev => {
    const next = new Set(prev)
    next.add(articleId)
    return next
  })
  try {
    await likeArticle(articleId)
  } catch {
    // revert on failure
    setBoostedIds(prev => {
      const next = new Set(prev)
      next.delete(articleId)
      return next
    })
  }
}

const handleDampen = async (articleId: number, e: React.MouseEvent) => {
  e.preventDefault()
  e.stopPropagation()
  // Optimistic remove
  setRecommended(prev => prev.filter(a => a.id !== articleId))
  try {
    await dislikeArticle(articleId)
    // Backfill: reuse the existing loadRecommended helper (wraps getRecommended(10))
    await loadRecommended()
  } catch {
    // No revert on dislike failure — the user wanted it gone, and a stale dislike
    // signal is mostly harmless. The next page reload will resync from the server.
  }
}
```

The dampen handler reuses the existing `loadRecommended` helper rather than duplicating the fetch — it already wraps `getRecommended(10)` and sets `recommended` state on success, with try/catch.

**JSX update** — add an action area as the row's right column:

```tsx
{showRecommended && recommended.map(article => (
  <Link key={article.id} to={`/articles/${article.id}`} className="rec-row">
    <div className="flex-between">
      <div style={{ flex: 1 }}>
        {/* ... existing title + meta ... */}
      </div>
      <div className="rec-feedback">
        {boostedIds.has(article.id) ? (
          <span className="rec-feedback-badge">✓ 已多推</span>
        ) : (
          <>
            <button
              type="button"
              className="rec-feedback-btn"
              title="多推这类"
              aria-label="多推这类"
              onClick={(e) => handleBoost(article.id, e)}
            >👍</button>
            <button
              type="button"
              className="rec-feedback-btn"
              title="少推这类"
              aria-label="少推这类"
              onClick={(e) => handleDampen(article.id, e)}
            >👎</button>
          </>
        )}
      </div>
    </div>
  </Link>
))}
```

The buttons use `e.preventDefault()` and `e.stopPropagation()` so a click on a button does not trigger the parent `<Link>`'s navigation.

#### `index.css`

```css
.rec-feedback {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-left: 12px;
  flex-shrink: 0;
  opacity: 0.5;
  transition: opacity 0.15s ease;
}

.rec-row:hover .rec-feedback {
  opacity: 1;
}

.rec-feedback-btn {
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 4px 6px;
  border-radius: 4px;
  font-size: 14px;
  line-height: 1;
  color: inherit;
}

.rec-feedback-btn:hover {
  background: #e3eaf7;
}

.rec-feedback-badge {
  font-size: 12px;
  color: #4b6bcc;
  padding: 2px 8px;
  background: #e3eaf7;
  border-radius: 4px;
  white-space: nowrap;
}
```

The `.rec-feedback` group sits at `opacity: 0.5` by default and brightens on row hover, so the buttons don't visually compete with the title on a quiet page but become obvious when the user is interacting.

## Data flow

### 多推

1. User clicks 👍 on a row.
2. `boostedIds` adds the article ID → buttons swap to `✓ 已多推` badge immediately.
3. `POST /api/preferences/like` fires.
4. Server records `like` signal, runs `applyCachedClassification` (+1.0 topic, +0.5 tag).
5. On success, no further UI change. On failure, the badge reverts to buttons.

### 少推

1. User clicks 👎 on a row.
2. Local `recommended` array filters out that article → row disappears immediately.
3. `POST /api/preferences/dislike` fires.
4. Server records `dislike` signal (-10 article score) and runs `dampenCachedClassification` (-0.5 topic, -0.25 tag, clamped at 0).
5. Client awaits the dislike response, then calls `getRecommended(10)` to refetch (matches the existing `loadRecommended` call site).
6. The disliked article is now excluded by `score > 0`, so the response includes the next-best article. The panel size returns to normal.

If step 3 fails, no revert: the user's intent was to remove the row. Worst case is a stale UI that resyncs on next page load.

## Error handling

- **Network failure on 多推**: revert the optimistic `boostedIds` change; user can retry.
- **Network failure on 少推**: silent — keep the row removed locally; next page load resyncs. The user wanted it gone.
- **Article not classified**: `dampenCachedClassification` no-ops; the per-article -10 score still hides the article from recs. Topic/tag profile unchanged. Acceptable.

## Testing

Manual verification (post-Docker rebuild — frontend changes need `docker-compose up -d --build frontend`):

1. Open `/articles`, expand 为你推荐, hover a row → buttons brighten to full opacity.
2. Click 👍 → buttons turn into `✓ 已多推` badge. Verify the row's `<Link>` navigation does not fire (URL stays on `/articles`).
3. Reload page → boost is preserved server-side (the like signal is in the DB), but the badge resets (badge state is session-only). Acceptable.
4. Click 👎 on a row → row fades out, panel refills with a new article. Confirm the disliked article does not reappear after refresh.
5. Confirm `interest_topics` and `interest_tags` weights drop appropriately by inspecting the relevant rows in the DB after a 少推 (only when the article has a cached classification).

No automated tests are added — the existing repo has no frontend test harness, and the backend changes are thin enough that manual DB inspection covers the contract.

## Files touched

- `backend/internal/repository/preference.go` — `DampenTopic`, `DampenTag` methods (~30 lines).
- `backend/internal/api/preference.go` — `dampenCachedClassification` helper + `Dislike` handler call (~20 lines).
- `frontend/src/pages/ArticleListPage.tsx` — `boostedIds` state, two handlers, JSX additions (~50 lines).
- `frontend/src/index.css` — `.rec-feedback`, `.rec-feedback-btn`, `.rec-feedback-badge` (~25 lines).
