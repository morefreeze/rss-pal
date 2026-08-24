# AI Summary Anchor Bounds Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Require 3–30 article links in newly generated multi-section detailed summaries, deploy the prompt change, and regenerate the two newest ready Hacker News and 科技爱好者 articles.

**Architecture:** Strengthen the existing system-owned `detailedArticleAnchorInstruction`; all detailed prompt paths already append this single constant, so no generation routing changes are needed. Production regeneration uses the normal authenticated article-page streaming endpoint, followed by database and rendered-target verification for exactly four selected articles.

**Tech Stack:** Go 1.25, React/TypeScript, PostgreSQL, GitHub Actions, Docker Compose, agent-browser

---

## File Structure

- Modify `backend/internal/ai/article_anchors.go`: replace the permissive anchor instruction with the 0-or-3–30 contract.
- Modify `backend/internal/ai/summarizer_anchor_prompt_test.go`: assert every required bound, ordering, grouping, and exception phrase.
- Verify existing stream/vision/template prompt-capture tests: prove the shared instruction still reaches every detailed path and no brief path.
- No production script is added: deployment uses the existing Tencent workflow and regeneration uses the existing authenticated UI/API.

### Task 1: Strengthen the System-Owned Anchor Prompt

**Files:**
- Modify: `backend/internal/ai/article_anchors.go:11-14`
- Modify: `backend/internal/ai/summarizer_anchor_prompt_test.go:9-31`

- [ ] **Step 1: Write a failing prompt-contract test**

Replace the loose phrase list with explicit assertions:

```go
func TestDetailedArticleAnchorInstructionRequiresBoundedLinks(t *testing.T) {
	required := []string{
		"按原文顺序",
		"按大意合并",
		"至少 3 个、最多 30 个",
		"每个总结组或段落至多一个",
		"必须来自正文中已有的锚点",
		"不要为了凑数",
		"短文或整篇只讲一件事",
		"可以完全不添加",
		"不得只添加 1 或 2 个",
	}
	for _, phrase := range required {
		if !strings.Contains(detailedArticleAnchorInstruction, phrase) {
			t.Errorf("instruction missing %q", phrase)
		}
	}
}
```

Also assert the instruction contains the strict Markdown example `[查看原文](#article-section-NNN)` and says links are distributed in source order.

- [ ] **Step 2: Run the focused test and verify RED**

Run: `cd backend && go test ./internal/ai -run TestDetailedArticleAnchorInstructionRequiresBoundedLinks -count=1`

Expected: FAIL because the existing prompt does not require 3–30 links and still permits any non-zero count.

- [ ] **Step 3: Implement the minimal prompt change**

Set the shared constant to:

```go
const detailedArticleAnchorInstruction = `请尽量按原文顺序总结，并按大意合并相邻内容。若文章包含多个明显章节或主题组，详细总结必须添加至少 3 个、最多 30 个 [查看原文](#article-section-NNN) 链接；每个总结组或段落至多一个，并按正文顺序分布。NNN 必须来自正文中已有的锚点。不要为了凑数给每个原文段落都添加链接，而应先形成有意义的总结组。短文或整篇只讲一件事时可以完全不添加链接；除此例外，不得只添加 1 或 2 个链接。`
```

Do not add retry, output rewriting, or brief-summary instructions.

- [ ] **Step 4: Run focused and full backend tests**

Run: `cd backend && go test ./internal/ai -count=1 && go test ./...`

Expected: all Go packages PASS; existing captured default/template/stream/vision detailed prompts retain the shared suffix and brief prompts remain free of it.

- [ ] **Step 5: Commit the prompt change**

```bash
git add backend/internal/ai/article_anchors.go backend/internal/ai/summarizer_anchor_prompt_test.go
git commit -m "fix(ai): require bounded article summary anchors"
```

### Task 2: Review, Merge, Push, and Deploy

**Files:**
- Verify only.

- [ ] **Step 1: Run repository verification**

Run:

```bash
cd backend && go test ./...
cd ../frontend && npm run check && npm run build
```

Expected: all backend tests, Vitest tests, legacy tests, and the production build PASS.

- [ ] **Step 2: Review the exact diff**

Run: `git diff --check origin/master..HEAD && git diff --stat origin/master..HEAD`

Expected: only the design, plan, prompt constant, and prompt tests differ.

- [ ] **Step 3: Obtain restart-boundary confirmation**

Show that `git push origin master` triggers `.github/workflows/deploy-tencent.yml`, whose deploy step runs `sudo /usr/local/sbin/rss-pal-deploy-from-actions`. Wait for explicit confirmation before pushing.

- [ ] **Step 4: Merge and push**

After confirmation, fast-forward `master`, rerun tests on the merged result, and run `git push origin master`.

- [ ] **Step 5: Verify Tencent deployment**

Wait for the exact workflow run to succeed. Then verify:

```bash
ssh tencent-rss-pal 'cd /opt/rss-pal && git rev-parse HEAD && docker compose ps'
curl --noproxy '*' -fsS https://rss.morefreeze.top/api/health
```

Expected: runtime HEAD equals the pushed commit; API, worker, and frontend are running; local/public health return success.

### Task 3: Select and Regenerate Exactly Four Production Articles

**Files:**
- Production data only; no repository files.

- [ ] **Step 1: Resolve exact feeds and candidates read-only**

Run a production PostgreSQL read-only query equivalent to:

```sql
WITH ranked AS (
  SELECT a.id, a.title, a.published_at, a.fetched_at, a.content,
         a.summary_detailed, f.title AS feed_title,
         row_number() OVER (
           PARTITION BY f.id
           ORDER BY a.published_at DESC NULLS LAST, a.id DESC
         ) AS rn
  FROM articles a
  JOIN feeds f ON f.id = a.feed_id
  WHERE f.title IN ('Hacker News', '科技爱好者')
    AND a.processing_state = 'ready'
    AND length(coalesce(a.content, '')) > 0
)
SELECT feed_title, id, title, published_at, fetched_at,
       length(content) AS content_chars,
       md5(coalesce(summary_detailed, '')) AS previous_summary_md5
FROM ranked
WHERE rn <= 2
ORDER BY feed_title, rn;
```

Expected: exactly two rows per feed. If feed titles differ, inspect feed rows and resolve the intended exact feeds before selecting; do not broaden to fuzzy unrelated sources.

- [ ] **Step 2: Classify article structure before mutation**

For each selected body, count canonical addressable blocks and inspect headings/topic groups. Record `multi-section` or `single-theme` with evidence. Do not infer eligibility only from character count.

- [ ] **Step 3: Regenerate sequentially through the authenticated UI**

Use the user's authenticated browser session and the article page's existing “重新生成” action for each selected ID. Wait for the streaming request to finish and the stored article to read back before moving to the next article. Do not send parallel generation requests.

- [ ] **Step 4: Verify stored output bounds**

Run a read-only query that reselects the same four candidates and returns the new summary hash and strict reference count:

```sql
WITH ranked AS (
  SELECT a.id, a.title, a.summary_detailed, f.title AS feed_title,
         row_number() OVER (
           PARTITION BY f.id
           ORDER BY a.published_at DESC NULLS LAST, a.id DESC
         ) AS rn
  FROM articles a
  JOIN feeds f ON f.id = a.feed_id
  WHERE f.title IN ('Hacker News', '科技爱好者')
    AND a.processing_state = 'ready'
    AND length(coalesce(a.content, '')) > 0
)
SELECT feed_title, id, title,
       md5(coalesce(summary_detailed, '')) AS new_summary_md5,
       (length(coalesce(summary_detailed, '')) -
        length(replace(coalesce(summary_detailed, ''), '#article-section-', ''))) /
       length('#article-section-') AS anchor_count
FROM ranked
WHERE rn <= 2
ORDER BY feed_title, rn;
```

Expected: each multi-section article has 3–30 references; a proven single-theme article may have zero; 1–2 is always invalid.

- [ ] **Step 5: Verify rendered targets and report**

For every generated `#article-section-NNN` reference, inspect the real article page DOM and assert exactly one matching target exists. Report feed, article ID/title, prior/new hash difference, structure classification, anchor count, target coverage, HTTP result, and any model non-compliance. Do not silently regenerate extra articles if acceptance fails.
