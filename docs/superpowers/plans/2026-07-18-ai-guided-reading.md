# AI Guided Reading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add manually generated, persistently anchored AI/user annotations to RSS Pal articles, with editable ownership, reader settings, responsive rendering, private user notes, and AI-only sharing/export.

**Architecture:** Store annotations separately from `articles.content` behind RLS. Parse Markdown into normalized text blocks on the backend, validate AI quotes against those blocks, and send text-quote selectors to a shared React Markdown decoration layer used by normal, immersive, and public-share reading surfaces. Keep generation atomic by validating a complete batch before replacing only still-AI-owned rows.

**Tech Stack:** Go 1.25, Gin, PostgreSQL 15 with RLS, `database/sql`, Goldmark, React 18, TypeScript, `react-markdown`/rehype, Vitest, Testing Library, CSS responsive layout.

---

## Scope and sequencing

This is one end-to-end feature rather than independent projects: the frontend
renderer depends on the selector contract, and public sharing depends on the
same ownership rules as generation. Implement tasks in order. Every task ends
with a focused commit and leaves the repository buildable.

Before Task 1, create or enter a dedicated feature worktree. Do not run these
steps on the user's dirty `master` checkout:

```bash
git worktree add .worktrees/ai-guided-reading -b feat/ai-guided-reading
cd .worktrees/ai-guided-reading
```

Expected: the new worktree starts at commit `9e2ee9d` or a descendant that
contains the approved design and this plan.

## File map

### Backend: create

- `backend/migrations/037_ai_guided_reading.sql` — settings and annotation
  tables, constraints, indexes, RLS policies, grants.
- `backend/internal/model/annotation.go` — shared database/API types and enums.
- `backend/internal/annotation/document.go` — Markdown-to-visible-block
  normalization, source hash, selector creation, selector resolution,
  fingerprints, density targets.
- `backend/internal/annotation/document_test.go` — pure anchoring contract.
- `backend/internal/ai/guided_reading.go` — prompt/JSON client on
  `*ai.Summarizer`.
- `backend/internal/ai/guided_reading_test.go` — prompt/parser contract.
- `backend/internal/service/guided_reading.go` — chunking, model orchestration,
  validation, dedupe, overlap filtering, generation batches.
- `backend/internal/service/guided_reading_test.go` — fake-model orchestration
  tests.
- `backend/internal/repository/annotation.go` — RLS-scoped settings, CRUD,
  ownership transitions, tombstones, atomic AI replacement, public projection.
- `backend/internal/repository/annotation_test.go` — repository and transaction
  tests.
- `backend/internal/api/annotation.go` — authenticated settings/annotation
  handlers and manual generation.
- `backend/internal/api/annotation_test.go` — HTTP status, ownership, and
  generation tests.
- `backend/internal/api/share_annotation_test.go` — public projection tests.
- `backend/internal/api/content_annotation_test.go` — Markdown export tests.

### Backend: modify

- `backend/go.mod`, `backend/go.sum` — promote Goldmark to a direct dependency.
- `backend/internal/repository/rls_migration_test.go` — require RLS on both new
  tables.
- `backend/internal/repository/rls_leak_test.go` — include both tables in the
  cross-user private-table matrix.
- `backend/internal/api/article.go` — factor a reusable user-specific
  `*ai.Summarizer` constructor.
- `backend/internal/api/article_summary_media_test.go` — preserve the vision
  model behavior after that refactor.
- `backend/internal/api/rls_http_leak_test.go` — prove annotation HTTP routes
  cannot cross private-feed ownership.
- `backend/internal/api/share.go` — include source content and public AI
  annotations.
- `backend/internal/api/content.go` — add the AI-guided-reading export section.
- `backend/cmd/server/main.go` — construct repository/handler and register
  routes.

### Frontend: create

- `frontend/vitest.config.ts` — jsdom unit/component test configuration.
- `frontend/src/annotations/types.ts` — UI-facing annotation action types.
- `frontend/src/annotations/rehypeAnnotations.ts` — HAST block indexing,
  selector resolution, decorated anchor/slot nodes.
- `frontend/src/annotations/selection.ts` — DOM Selection to one-block selector
  input.
- `frontend/src/annotations/AnnotationContext.tsx` — render/edit action context.
- `frontend/src/components/AnnotationCard.tsx` — label, display, edit, delete.
- `frontend/src/components/AnnotationSidebar.tsx` — measured desktop card
  placement.
- `frontend/src/components/AnnotationSelectionToolbar.tsx` — three creation
  actions and comment editor.
- `frontend/src/components/AIReadingSettingsCard.tsx` — AI-tab settings card.
- `frontend/src/hooks/useArticleAnnotations.ts` — fetch, generate, create,
  update, delete, and optimistic/local state.
- `frontend/test/annotationResolver.test.tsx` — quote resolution/render tests.
- `frontend/test/annotationSelection.test.ts` — one-block selection tests.
- `frontend/test/annotationSelectionToolbar.test.tsx` — captured-range and
  create/undo interaction tests.
- `frontend/test/useArticleAnnotations.test.tsx` — visibility, mutation failure,
  and stale-request hook tests.
- `frontend/test/annotationCard.test.tsx` — ownership/edit tests.
- `frontend/test/aiReadingSettings.test.tsx` — settings defaults and saves.
- `frontend/test/articleAnnotationsIntegration.test.tsx` — article/reading-mode
  wiring contract.
- `frontend/test/shareAnnotations.test.tsx` — public read-only projection.

### Frontend: modify

- `frontend/package.json`, `frontend/package-lock.json` — Vitest/jsdom/Testing
  Library, synchronous SHA-256 support, and a real `npm test` script.
- `frontend/src/api/client.ts` — settings/annotation types and API calls.
- `frontend/src/components/MarkdownArticle.tsx` — optional annotation plugin,
  anchor fragments, inline slots, shared frame, editable selection events.
- `frontend/src/components/TweetCard.tsx` — reuse annotated Markdown for tweet
  detail bodies while preserving the byline offset and X link transform.
- `frontend/src/components/ReadingLayout.tsx` — accept and render the shared
  annotation state.
- `frontend/src/pages/ArticlePage.tsx` — load annotations, add manual generation
  action, pass shared actions to both reading surfaces.
- `frontend/src/pages/SettingsPage.tsx` — mount the focused settings card in the
  existing AI tab.
- `frontend/src/pages/SharePage.tsx` — render public content with read-only AI
  annotations.
- `frontend/src/index.css` — evaluation/highlight/term styles, sidebar layout,
  inline mobile layout, selection toolbar, focus states.
- `frontend/test/readingProgress.test.ts` — migrate the existing top-level
  assertions into Vitest cases.
- `frontend/test/popularFeedsVisibility.test.ts` — migrate the existing
  top-level assertions into Vitest cases.

## Task 1: Schema, RLS, and model contract

**Files:**
- Create: `backend/migrations/037_ai_guided_reading.sql`
- Create: `backend/internal/model/annotation.go`
- Modify: `backend/internal/repository/rls_migration_test.go`
- Modify: `backend/internal/repository/rls_leak_test.go`

- [ ] **Step 1: Write failing migration and RLS tests**

Add this test to `rls_migration_test.go`:

```go
func TestMigration037_GuidedReadingTablesHaveRLS(t *testing.T) {
	db, cleanup := testdb.New(t)
	defer cleanup()

	for _, table := range []string{"ai_reading_settings", "article_annotations"} {
		var enabled, forced bool
		if err := db.QueryRow(`
			SELECT relrowsecurity, relforcerowsecurity
			FROM pg_class
			WHERE relname = $1 AND relkind = 'r'`, table).Scan(&enabled, &forced); err != nil {
			t.Fatalf("%s: lookup: %v", table, err)
		}
		if !enabled || !forced {
			t.Fatalf("%s: enabled=%v forced=%v, want both true", table, enabled, forced)
		}
		var policies int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pg_policies WHERE tablename = $1`, table).Scan(&policies); err != nil {
			t.Fatalf("%s: policies: %v", table, err)
		}
		if policies != 1 {
			t.Fatalf("%s: policies=%d, want 1", table, policies)
		}
	}
}
```

Add these cases to `TestRLS_PrivateTablesAreScoped`:

```go
{
	name:     "ai_reading_settings",
	seedSQL:  `INSERT INTO ai_reading_settings (user_id) VALUES ($2)`,
	countSQL: `SELECT COUNT(*) FROM ai_reading_settings`,
},
{
	name: "article_annotations",
	seedSQL: `INSERT INTO article_annotations (
		user_id, article_id, kind, comment, author_kind, origin_kind,
		source_hash, block_index, start_offset, end_offset,
		quote_exact, quote_prefix, quote_suffix, fingerprint
	) VALUES ($2, $1, 'highlight', '', 'user', 'user',
		repeat('a', 64), 0, 0, 1, 'x', '', '', repeat('b', 64))`,
	countSQL: `SELECT COUNT(*) FROM article_annotations`,
},
```

- [ ] **Step 2: Run the tests and verify the missing-table failure**

Run:

```bash
cd backend
go test ./internal/repository -run 'TestMigration037|TestRLS_PrivateTablesAreScoped' -count=1
```

Expected: FAIL because `ai_reading_settings` and `article_annotations` do not
exist. If the DB fixture skips because `TEST_DATABASE_URL` is unavailable,
configure the local Postgres test DSN before continuing; do not treat a skip as
the red phase.

- [ ] **Step 3: Add the migration**

Create `037_ai_guided_reading.sql` with this complete schema:

```sql
CREATE TABLE IF NOT EXISTS ai_reading_settings (
    user_id INT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    density VARCHAR(16) NOT NULL DEFAULT 'standard'
        CHECK (density IN ('sparse', 'standard', 'dense')),
    evaluation_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    term_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS article_annotations (
    id BIGSERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    article_id INT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL
        CHECK (kind IN ('highlight', 'evaluation', 'term')),
    comment TEXT NOT NULL DEFAULT '',
    author_kind VARCHAR(8) NOT NULL
        CHECK (author_kind IN ('ai', 'user')),
    origin_kind VARCHAR(8) NOT NULL
        CHECK (origin_kind IN ('ai', 'user')),
    generation_id VARCHAR(64),
    source_hash CHAR(64) NOT NULL,
    block_index INT NOT NULL CHECK (block_index >= 0),
    start_offset INT NOT NULL CHECK (start_offset >= 0),
    end_offset INT NOT NULL CHECK (end_offset > start_offset),
    quote_exact TEXT NOT NULL CHECK (length(btrim(quote_exact)) > 0),
    quote_prefix TEXT NOT NULL DEFAULT '',
    quote_suffix TEXT NOT NULL DEFAULT '',
    fingerprint CHAR(64) NOT NULL,
    dismissed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CHECK (
        (kind = 'highlight' AND comment = '') OR
        (kind IN ('evaluation', 'term') AND length(btrim(comment)) > 0)
    )
);

CREATE INDEX IF NOT EXISTS idx_article_annotations_owner_article
    ON article_annotations(user_id, article_id, dismissed);
CREATE INDEX IF NOT EXISTS idx_article_annotations_generation
    ON article_annotations(generation_id) WHERE generation_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_article_annotations_fingerprint
    ON article_annotations(user_id, article_id, fingerprint);

ALTER TABLE ai_reading_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE ai_reading_settings FORCE ROW LEVEL SECURITY;
CREATE POLICY ai_reading_settings_user_isolation ON ai_reading_settings
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

ALTER TABLE article_annotations ENABLE ROW LEVEL SECURITY;
ALTER TABLE article_annotations FORCE ROW LEVEL SECURITY;
CREATE POLICY article_annotations_user_isolation ON article_annotations
    USING (app_rls_bypass() OR user_id = app_current_user_id())
    WITH CHECK (app_rls_bypass() OR user_id = app_current_user_id());

GRANT SELECT, INSERT, UPDATE, DELETE ON ai_reading_settings TO rsspal_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON article_annotations TO rsspal_app;
GRANT USAGE, SELECT ON SEQUENCE article_annotations_id_seq TO rsspal_app;
```

- [ ] **Step 4: Add the shared model types**

Create `backend/internal/model/annotation.go`:

```go
package model

import "time"

type AnnotationKind string

const (
	AnnotationHighlight  AnnotationKind = "highlight"
	AnnotationEvaluation AnnotationKind = "evaluation"
	AnnotationTerm       AnnotationKind = "term"
)

type AnnotationAuthor string

const (
	AnnotationAuthorAI   AnnotationAuthor = "ai"
	AnnotationAuthorUser AnnotationAuthor = "user"
)

type AIReadingSettings struct {
	UserID            int       `json:"-"`
	Enabled           bool      `json:"enabled"`
	Density           string    `json:"density"`
	EvaluationEnabled bool      `json:"evaluation_enabled"`
	TermEnabled       bool      `json:"term_enabled"`
	CreatedAt         time.Time `json:"created_at,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

func DefaultAIReadingSettings(userID int) AIReadingSettings {
	return AIReadingSettings{
		UserID: userID, Enabled: true, Density: "standard",
		EvaluationEnabled: true, TermEnabled: true,
	}
}

type ArticleAnnotation struct {
	ID           int64            `json:"id"`
	UserID       int              `json:"-"`
	ArticleID    int              `json:"article_id"`
	Kind         AnnotationKind   `json:"kind"`
	Comment      string           `json:"comment"`
	AuthorKind   AnnotationAuthor `json:"author_kind"`
	OriginKind   AnnotationAuthor `json:"origin_kind"`
	GenerationID *string          `json:"-"`
	SourceHash   string           `json:"source_hash"`
	BlockIndex   int              `json:"block_index"`
	StartOffset  int              `json:"start_offset"`
	EndOffset    int              `json:"end_offset"`
	QuoteExact   string           `json:"quote_exact"`
	QuotePrefix  string           `json:"quote_prefix"`
	QuoteSuffix  string           `json:"quote_suffix"`
	Fingerprint  string           `json:"-"`
	Dismissed    bool             `json:"-"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    time.Time        `json:"updated_at"`
}
```

- [ ] **Step 5: Re-run focused RLS tests**

Run the command from Step 2.

Expected: PASS for both new tables and the private-table matrix.

- [ ] **Step 6: Commit the schema contract**

```bash
git add backend/migrations/037_ai_guided_reading.sql \
  backend/internal/model/annotation.go \
  backend/internal/repository/rls_migration_test.go \
  backend/internal/repository/rls_leak_test.go
git commit -m "feat: add guided reading annotation schema"
```

## Task 2: Markdown document and selector domain

**Files:**
- Create: `backend/internal/annotation/document.go`
- Create: `backend/internal/annotation/document_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

- [ ] **Step 1: Write the selector contract tests**

Create table-driven tests covering this public API:

```go
func TestParseDocumentVisibleBlocks(t *testing.T) {
	doc := ParseDocument("# 标题\n\n第一段 **重点** 内容。\n\n- 列表项")
	if got, want := len(doc.Blocks), 3; got != want { t.Fatalf("blocks=%d want=%d", got, want) }
	if doc.Blocks[1].Text != "第一段 重点 内容。" { t.Fatalf("text=%q", doc.Blocks[1].Text) }
	if doc.Blocks[2].Text != "列表项" { t.Fatalf("list text=%q", doc.Blocks[2].Text) }
	if len(doc.Hash) != 64 { t.Fatalf("hash=%q", doc.Hash) }
}

func TestSelectorFromQuoteAndResolveAfterSmallEdit(t *testing.T) {
	oldDoc := ParseDocument("前段。\n\n真正稀缺的是判断能力。\n\n后段。")
	sel, err := SelectorFromQuote(oldDoc, 1, "判断能力")
	if err != nil { t.Fatal(err) }
	newDoc := ParseDocument("新增开头。\n\n前段。\n\n真正稀缺的是判断能力。\n\n后段。")
	got, err := ResolveSelector(newDoc, sel)
	if err != nil { t.Fatal(err) }
	if newDoc.Blocks[got.BlockIndex].Text[got.StartOffset:got.EndOffset] != "判断能力" {
		t.Fatalf("resolved=%+v", got)
	}
}

func TestResolveSelectorRejectsAmbiguousQuote(t *testing.T) {
	doc := ParseDocument("同一句。\n\n同一句。")
	_, err := ResolveSelector(doc, Selector{Exact: "同一句"})
	if !errors.Is(err, ErrAmbiguousSelector) { t.Fatalf("err=%v", err) }
}

func TestTargetCount(t *testing.T) {
	if got := TargetCount(1000, "sparse"); got != 3 { t.Fatalf("sparse=%d", got) }
	if got := TargetCount(2000, "standard"); got != 12 { t.Fatalf("standard=%d", got) }
	if got := TargetCount(10000, "dense"); got != 30 { t.Fatalf("dense cap=%d", got) }
}
```

Use rune slices in the real assertion rather than byte slicing for Chinese:
`string([]rune(text)[start:end])`.

Add `TestParseDocumentNestedAndLooseLists` with a tight nested list expecting
`["外层", "内层"]` and a loose list item with two paragraphs expecting each
paragraph as its own consecutive block. Reuse these expected block arrays in
the frontend resolver fixtures so both implementations prove the same ordinal
contract.

- [ ] **Step 2: Run the tests and verify the package/dependency failure**

```bash
cd backend
go test ./internal/annotation -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Promote Goldmark to a direct dependency**

```bash
cd backend
go get github.com/yuin/goldmark@v1.7.13
```

Expected: `go.mod` gains a direct Goldmark requirement and `go.sum` remains
consistent.

- [ ] **Step 4: Implement the document API**

Create `document.go` with these exact exported contracts:

```go
package annotation

type Block struct {
	Index int
	Text  string
}

type Document struct {
	Blocks       []Block
	Hash         string
	VisibleRunes int
}

type Selector struct {
	SourceHash  string
	BlockIndex  int
	StartOffset int
	EndOffset   int
	Exact       string
	Prefix      string
	Suffix      string
}

type ResolvedRange struct {
	BlockIndex  int
	StartOffset int
	EndOffset   int
}

var (
	ErrQuoteNotFound     = errors.New("annotation quote not found")
	ErrAmbiguousSelector = errors.New("annotation selector is ambiguous")
	ErrInvalidRange      = errors.New("annotation range is invalid")
)

func ParseDocument(markdown string) Document
func SelectorFromQuote(doc Document, blockIndex int, exact string) (Selector, error)
func SelectorFromOffsets(doc Document, blockIndex, start, end int, exact string) (Selector, error)
func ResolveSelector(doc Document, sel Selector) (ResolvedRange, error)
func Fingerprint(kind string, sel Selector) string
func TargetCount(visibleRunes int, density string) int
```

Implementation rules:

```go
const contextRunes = 48

func normalizeVisibleText(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\u00a0", " ")), " ")
}

func TargetCount(n int, density string) int {
	rate := map[string]int{"sparse": 3, "standard": 6, "dense": 10}[density]
	if rate == 0 { rate = 6 }
	if n <= 0 { return 0 }
	target := int(math.Round(float64(n) / 1000 * float64(rate)))
	if target < 1 { target = 1 }
	if target > 30 { target = 30 }
	return target
}
```

Parse with `goldmark.New(goldmark.WithExtensions(extension.GFM))`. Walk heading,
paragraph, and `ast.KindTextBlock` nodes in document order; Goldmark uses
`TextBlock` for tight-list item text, so omitting it would violate the approved
list-item contract and the test above. Recursively concatenate inline text,
code spans, and soft/hard line breaks, normalize whitespace, and omit empty
blocks. Compute the document hash as SHA-256 of block text joined with
`"\n\n"`.

Selector creation must use rune offsets, derive bounded prefix/suffix from the
canonical block, and require the supplied exact text to equal the selected
canonical range. `SelectorFromQuote` accepts exactly one occurrence in the
declared block and returns `ErrAmbiguousSelector` for duplicates;
`SelectorFromOffsets` uses the explicit user-selected occurrence. Resolution
order must be: saved position + exact assertion;
saved block + context-scored exact occurrence; global context-scored exact
occurrence accepted only for one best match. Gate the saved-position shortcut
on `sel.SourceHash == doc.Hash`. Score a candidate as the number of equal
trailing runes between saved/current prefix plus equal leading runes between
saved/current suffix; accept a stage only when exactly one candidate has the
highest score (a sole occurrence wins even with score zero). A top-score tie is
ambiguous: at the saved-block stage continue to the global stage; at the global
stage return `ErrAmbiguousSelector`. Implement the same scoring rule in the
frontend resolver. Fingerprints are SHA-256 of
`kind + "\x00" + exact + "\x00" + prefix + "\x00" + suffix`.

- [ ] **Step 5: Run and pass the pure package tests**

```bash
cd backend
go test ./internal/annotation -count=1
```

Expected: PASS, including Chinese rune offsets and ambiguity rejection.

- [ ] **Step 6: Commit the anchor domain**

```bash
git add backend/go.mod backend/go.sum backend/internal/annotation
git commit -m "feat: add guided reading text selectors"
```

## Task 3: AI prompt and JSON client

**Files:**
- Create: `backend/internal/ai/guided_reading.go`
- Create: `backend/internal/ai/guided_reading_test.go`

- [ ] **Step 1: Write failing prompt/parser tests**

Cover fence removal, allowed kinds, source block IDs, and malformed JSON:

```go
func TestParseGuidedReadingJSON(t *testing.T) {
	raw := "```json\n{\"annotations\":[{\"block_index\":2,\"quote_exact\":\"注意力经济\",\"kind\":\"term\",\"comment\":\"一种分析框架\"}]}\n```"
	got, err := ParseGuidedReadingJSON(raw)
	if err != nil { t.Fatal(err) }
	if len(got) != 1 || got[0].BlockIndex != 2 || got[0].Kind != "term" {
		t.Fatalf("got=%+v", got)
	}
}

func TestBuildGuidedReadingPromptUsesVerbatimContract(t *testing.T) {
	p := BuildGuidedReadingPrompt(GuidedReadingRequest{
		Title: "标题", Target: 2,
		AllowedKinds: []string{"highlight", "term"},
		Blocks: []GuidedReadingBlock{{Index: 4, Text: "原文句子。"}},
	})
	for _, want := range []string{"[block:4]", "逐字复制", "highlight", "term", `"annotations"`} {
		if !strings.Contains(p, want) { t.Fatalf("prompt missing %q", want) }
	}
}
```

- [ ] **Step 2: Run and verify the red phase**

```bash
cd backend
go test ./internal/ai -run GuidedReading -count=1
```

Expected: FAIL with undefined guided-reading symbols.

- [ ] **Step 3: Implement the prompt, parser, and Summarizer method**

Use these types and method signature:

```go
type GuidedReadingBlock struct {
	Index int
	Text  string
}

type GuidedReadingRequest struct {
	Title        string
	Blocks       []GuidedReadingBlock
	AllowedKinds []string
	Target       int
}

type GuidedReadingCandidate struct {
	BlockIndex int    `json:"block_index"`
	QuoteExact string `json:"quote_exact"`
	Kind       string `json:"kind"`
	Comment    string `json:"comment"`
}

type guidedReadingEnvelope struct {
	Annotations []GuidedReadingCandidate `json:"annotations"`
}

func BuildGuidedReadingPrompt(req GuidedReadingRequest) string
func ParseGuidedReadingJSON(raw string) ([]GuidedReadingCandidate, error)
func (s *Summarizer) GenerateGuidedReading(ctx context.Context, req GuidedReadingRequest) ([]GuidedReadingCandidate, error)
```

The prompt must state in Chinese that quotes are copied verbatim from exactly
one numbered block, comments are Chinese, `highlight` has an empty comment,
disabled kinds are forbidden, and the response is only this JSON shape:

```json
{"annotations":[{"block_index":0,"quote_exact":"原文","kind":"evaluation","comment":"评价"}]}
```

`ParseGuidedReadingJSON` trims one Markdown fence, rejects an empty annotation
array, and leaves semantic quote/type validation to the service. The model call
uses `s.call` with `maxTokens = min(6000, 800 + req.Target*220)`.

- [ ] **Step 4: Re-run focused AI tests**

Run the command from Step 2.

Expected: PASS.

- [ ] **Step 5: Commit the AI client**

```bash
git add backend/internal/ai/guided_reading.go backend/internal/ai/guided_reading_test.go
git commit -m "feat: add guided reading AI prompt"
```

## Task 4: Generation orchestration and validation

**Files:**
- Create: `backend/internal/service/guided_reading.go`
- Create: `backend/internal/service/guided_reading_test.go`

- [ ] **Step 1: Write fake-generator tests first**

Define a fake that records requests and returns candidates. Cover:

```go
type fakeGuidedReadingGenerator struct {
	responses [][]ai.GuidedReadingCandidate
	err       error
	requests  []ai.GuidedReadingRequest
}

func (f *fakeGuidedReadingGenerator) GenerateGuidedReading(ctx context.Context, req ai.GuidedReadingRequest) ([]ai.GuidedReadingCandidate, error) {
	f.requests = append(f.requests, req)
	if f.err != nil { return nil, f.err }
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}
```

Required tests:

- `TestGenerateGuidedReadingValidatesQuotesAndComments`: fabricated quotes,
  disabled kinds, empty comments, and duplicates are dropped while valid rows
  get canonical selectors/fingerprints.
- `TestGenerateGuidedReadingRejectsEmptyValidBatch`: returns
  `ErrNoValidAnnotations`.
- `TestGenerateGuidedReadingChunksLongArticles`: every request is at most 8,000
  visible runes and total targets equal `annotation.TargetCount`.
- `TestGenerateGuidedReadingSkipsZeroAllocationChunks`: an article with more
  chunks than the absolute target cap makes at most 30 positive-target model
  calls and their targets sum to 30.
- `TestGenerateGuidedReadingFailsWholeBatchWhenChunkFails`: one model error
  returns an error and no batch.
- `TestGenerateGuidedReadingDropsHeavyOverlap`: retains the higher-priority
  comment row instead of multiple annotations over the same sentence.

- [ ] **Step 2: Run and verify undefined orchestration symbols**

```bash
cd backend
go test ./internal/service -run GuidedReading -count=1
```

Expected: FAIL.

- [ ] **Step 3: Implement the service contract**

Create these exact public types:

```go
type GuidedReadingGenerator interface {
	GenerateGuidedReading(context.Context, ai.GuidedReadingRequest) ([]ai.GuidedReadingCandidate, error)
}

type GuidedReadingBatch struct {
	GenerationID string
	SourceHash   string
	Annotations  []model.ArticleAnnotation
}

var ErrNoValidAnnotations = errors.New("no valid guided reading annotations")

func GenerateGuidedReading(
	ctx context.Context,
	generator GuidedReadingGenerator,
	article *model.Article,
	settings model.AIReadingSettings,
) (*GuidedReadingBatch, error)
```

Implementation sequence:

```go
doc := annotation.ParseDocument(article.Content)
target := annotation.TargetCount(doc.VisibleRunes, settings.Density)
if target == 0 { return nil, ErrNoValidAnnotations }

allowed := []string{string(model.AnnotationHighlight)}
if settings.EvaluationEnabled { allowed = append(allowed, string(model.AnnotationEvaluation)) }
if settings.TermEnabled { allowed = append(allowed, string(model.AnnotationTerm)) }
```

Split `doc.Blocks` at block boundaries into chunks of at most 8,000 visible
runes. Allocate the total target proportionally with largest-remainder
rounding. When the target is at least the number of chunks, give each non-empty
chunk at least one. When there are more chunks than the capped target, assign
one to the `target` chunks with the largest visible-rune counts and zero to the
rest. Never call the model for a zero-target chunk; every request target is
positive and all request targets sum exactly to `target`.

For every candidate: require an allowed kind, quote length 1-400 runes, comment
length 1-600 runes for comment kinds, empty comment for highlights, and a
successful `annotation.SelectorFromQuote`. Construct `model.ArticleAnnotation`
with `AuthorKind=ai`, `OriginKind=ai`, canonical selector fields, and
`annotation.Fingerprint`. Deduplicate fingerprints. For overlaps above 70% of
the shorter range, prefer `term`, then `evaluation`, then `highlight` so useful
comments beat a plain highlight. Trim to `target` and absolute cap 30.

Generate `GenerationID` from 16 crypto-random bytes encoded as hex. If any
chunk call fails or zero validated candidates survive, return an error without
a batch.

- [ ] **Step 4: Run service and dependent pure tests**

```bash
cd backend
go test ./internal/annotation ./internal/ai ./internal/service -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit generation orchestration**

```bash
git add backend/internal/service/guided_reading.go backend/internal/service/guided_reading_test.go
git commit -m "feat: validate guided reading generations"
```

## Task 5: RLS-scoped annotation repository

**Files:**
- Create: `backend/internal/repository/annotation.go`
- Create: `backend/internal/repository/annotation_test.go`

- [ ] **Step 1: Write repository tests before the repository**

Use `testdb.New(t)` to seed one user, one feed, and one article with content.
Cover these cases with real SQL:

```go
func TestAnnotationRepositorySettingsDefaultsAndUpsert(t *testing.T)
func TestAnnotationRepositoryCreateListAndTakeOwnership(t *testing.T)
func TestAnnotationRepositoryDeleteAIOriginCreatesTombstone(t *testing.T)
func TestAnnotationRepositoryDeleteUserOriginRemovesRow(t *testing.T)
func TestAnnotationRepositoryReplaceAIBatchPreservesUserRowsAndTombstones(t *testing.T)
func TestAnnotationRepositoryStaleContentPreservesOldBatch(t *testing.T)
func TestAnnotationRepositoryPublicProjectionExcludesUserOwnedAndDisabled(t *testing.T)
```

The atomic replacement fixture must contain:

```go
oldAI := model.ArticleAnnotation{AuthorKind: model.AnnotationAuthorAI, OriginKind: model.AnnotationAuthorAI}
editedAI := model.ArticleAnnotation{AuthorKind: model.AnnotationAuthorUser, OriginKind: model.AnnotationAuthorAI}
userNote := model.ArticleAnnotation{AuthorKind: model.AnnotationAuthorUser, OriginKind: model.AnnotationAuthorUser}
dismissed := model.ArticleAnnotation{AuthorKind: model.AnnotationAuthorUser, OriginKind: model.AnnotationAuthorAI, Dismissed: true}
```

After replacement, assert the old AI row is gone, the new AI batch exists,
`editedAI`, `userNote`, and `dismissed` still exist, and a new draft with the
dismissed fingerprint was filtered out.

- [ ] **Step 2: Run repository tests and verify the missing constructor**

```bash
cd backend
go test ./internal/repository -run AnnotationRepository -count=1
```

Expected: FAIL with `undefined: repository.NewAnnotationRepository`.

- [ ] **Step 3: Implement repository construction, settings, and scanning**

Create:

```go
type AnnotationRepository struct { db Querier }

func NewAnnotationRepository(db *sql.DB) *AnnotationRepository {
	return &AnnotationRepository{db: db}
}

func (r *AnnotationRepository) WithCtx(c ctxkey.CtxGetter) *AnnotationRepository {
	if v, ok := c.Get(ctxkey.Tx); ok {
		if q, ok := v.(Querier); ok { return &AnnotationRepository{db: q} }
	}
	return r
}

func (r *AnnotationRepository) GetSettings(userID int) (model.AIReadingSettings, error)
func (r *AnnotationRepository) UpsertSettings(*model.AIReadingSettings) error
func (r *AnnotationRepository) List(userID, articleID int) ([]model.ArticleAnnotation, error)
func (r *AnnotationRepository) ListPublicAI(userID, articleID int) ([]model.ArticleAnnotation, error)
func (r *AnnotationRepository) CreateUser(*model.ArticleAnnotation) error
func (r *AnnotationRepository) UpdateAndTakeOwnership(userID, articleID int, annotationID int64, kind model.AnnotationKind, comment string) (*model.ArticleAnnotation, error)
func (r *AnnotationRepository) DeleteOrDismiss(userID, articleID int, annotationID int64) error
func (r *AnnotationRepository) ReplaceAIBatchIfContentUnchanged(userID, articleID int, expectedContent, generationID string, annotations []model.ArticleAnnotation) (bool, error)
```

`GetSettings` returns `model.DefaultAIReadingSettings(userID)` on
`sql.ErrNoRows`. `UpsertSettings` validates density before this SQL:

```sql
INSERT INTO ai_reading_settings
    (user_id, enabled, density, evaluation_enabled, term_enabled)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (user_id) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    density = EXCLUDED.density,
    evaluation_enabled = EXCLUDED.evaluation_enabled,
    term_enabled = EXCLUDED.term_enabled,
    updated_at = NOW()
RETURNING created_at, updated_at
```

Use one `scanAnnotation(scanner interface{ Scan(...any) error })` helper so all
queries scan fields in this order:

```sql
id, user_id, article_id, kind, comment, author_kind, origin_kind,
generation_id, source_hash, block_index, start_offset, end_offset,
quote_exact, quote_prefix, quote_suffix, fingerprint, dismissed,
created_at, updated_at
```

`ListPublicAI` includes this predicate so a missing settings row means enabled:

```sql
author_kind = 'ai' AND dismissed = FALSE
AND COALESCE((SELECT enabled FROM ai_reading_settings WHERE user_id = $1), TRUE)
```

`List` filters by both `user_id` and `article_id`, excludes dismissed rows, and
orders by `block_index, start_offset, id`. `CreateUser` inserts every canonical
selector field supplied by the handler, forces both ownership columns to
`user`, forces `dismissed=FALSE`, and returns the row through
`scanAnnotation`; it never trusts ownership or fingerprint values from JSON.

- [ ] **Step 4: Implement ownership-aware mutations**

`UpdateAndTakeOwnership` uses:

```sql
UPDATE article_annotations
SET kind = $4, comment = $5, author_kind = 'user', updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND article_id = $3 AND dismissed = FALSE
RETURNING id, user_id, article_id, kind, comment, author_kind, origin_kind,
          generation_id, source_hash, block_index, start_offset, end_offset,
          quote_exact, quote_prefix, quote_suffix, fingerprint, dismissed,
          created_at, updated_at
```

Return `sql.ErrNoRows` when the row is not owned by the caller. For
`DeleteOrDismiss`, first read `origin_kind`. When it is `ai`, retain the row as
a tombstone:

```sql
UPDATE article_annotations
SET author_kind = 'user', dismissed = TRUE, updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND article_id = $3
```

When `origin_kind=user`, delete with all three identifiers in the WHERE clause.

- [ ] **Step 5: Implement atomic AI replacement with content CAS**

Use `txOrBegin(r.db)` and propagate every error. The method body follows this
order:

```go
tx, commit, rollback, err := txOrBegin(r.db)
if err != nil { return false, err }
defer rollback()

var current string
err = tx.QueryRow(`SELECT COALESCE(content, '') FROM articles WHERE id = $1`, articleID).Scan(&current)
if err != nil { return false, err }
if current != expectedContent { return false, nil }

rows, err := tx.Query(`SELECT fingerprint FROM article_annotations
	WHERE user_id=$1 AND article_id=$2 AND dismissed=TRUE`, userID, articleID)
if err != nil { return false, err }
tombstones := map[string]bool{}
for rows.Next() {
	var fingerprint string
	if err = rows.Scan(&fingerprint); err != nil { rows.Close(); return false, err }
	tombstones[fingerprint] = true
}
if err = rows.Err(); err != nil { rows.Close(); return false, err }
if err = rows.Close(); err != nil { return false, err }

if _, err = tx.Exec(`DELETE FROM article_annotations
	WHERE user_id=$1 AND article_id=$2 AND author_kind='ai'`, userID, articleID); err != nil {
	return false, err
}

for _, a := range annotations {
	if tombstones[a.Fingerprint] { continue }
	_, err = tx.Exec(`INSERT INTO article_annotations (
		user_id, article_id, kind, comment, author_kind, origin_kind,
		generation_id, source_hash, block_index, start_offset, end_offset,
		quote_exact, quote_prefix, quote_suffix, fingerprint, dismissed
	) VALUES ($1,$2,$3,$4,'ai','ai',$5,$6,$7,$8,$9,$10,$11,$12,$13,FALSE)`,
		userID, articleID, a.Kind, a.Comment, generationID, a.SourceHash,
		a.BlockIndex, a.StartOffset, a.EndOffset, a.QuoteExact,
		a.QuotePrefix, a.QuoteSuffix, a.Fingerprint)
	if err != nil { return false, err }
}
if err = commit(); err != nil { return false, err }
return true, nil
```

If `r.db` is already the request's `*sql.Tx`, `commit`/`rollback` are no-ops and
the handler must propagate the method error so `RLSTxMiddleware` rolls back.

- [ ] **Step 6: Run repository and RLS tests**

```bash
cd backend
go test ./internal/repository -run 'AnnotationRepository|TestMigration037|TestRLS_PrivateTablesAreScoped' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit repository behavior**

```bash
git add backend/internal/repository/annotation.go backend/internal/repository/annotation_test.go
git commit -m "feat: persist guided reading annotations"
```

## Task 6: Authenticated settings, CRUD, and generation API

**Files:**
- Create: `backend/internal/api/annotation.go`
- Create: `backend/internal/api/annotation_test.go`
- Modify: `backend/internal/api/article.go`
- Modify: `backend/internal/api/article_summary_media_test.go`
- Modify: `backend/internal/api/rls_http_leak_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Refactor the user Summarizer constructor under a passing test**

Change the existing helper without changing behavior:

```go
func newUserSummarizer(aiCfg *model.UserAIConfig, cfg *config.Config) *ai.Summarizer {
	if aiCfg == nil || aiCfg.APIKey == "" { return nil }
	baseURL := aiCfg.BaseURL
	if baseURL == "" && cfg != nil { baseURL = cfg.Claude.BaseURL }
	s := ai.NewSummarizerWithModel(aiCfg.APIKey, baseURL, aiCfg.Model)
	if cfg != nil && cfg.AI.Vision.Model != "" { s.SetVisionModel(cfg.AI.Vision.Model) }
	return s
}

func newUserSummarizerService(aiCfg *model.UserAIConfig, cfg *config.Config) *service.SummarizerService {
	s := newUserSummarizer(aiCfg, cfg)
	if s == nil { return nil }
	return service.NewSummarizerService(s)
}
```

Extend `TestUserAIConfigSummarizerCarriesVisionModel` to assert both helpers
return non-nil and preserve `vision-model`.

Run:

```bash
cd backend
go test ./internal/api -run UserAIConfigSummarizer -count=1
```

Expected: PASS before moving on.

- [ ] **Step 2: Write failing handler tests**

Use a real test database and a Gin router with `userID` set in context. Inject a
fake `service.GuidedReadingGenerator`. Cover:

```go
func TestAnnotationSettingsDefaultsAndValidation(t *testing.T)
func TestCreateAnnotationCanonicalizesSelector(t *testing.T)
func TestCreateAnnotationRejectsCrossBlockOrFabricatedQuote(t *testing.T)
func TestPatchAIAnnotationTakesOwnership(t *testing.T)
func TestDeleteAIAnnotationCreatesHiddenTombstone(t *testing.T)
func TestGenerateAnnotationsDisabledReturns409(t *testing.T)
func TestGenerateAnnotationsWithoutAIConfigReturns503(t *testing.T)
func TestGenerateAnnotationsModelFailurePreservesOldBatch(t *testing.T)
func TestGenerateAnnotationsSuccessReplacesOnlyAI(t *testing.T)
func TestGenerateAnnotationsContentChangedReturns409(t *testing.T)
```

Add GET/PATCH/DELETE annotation endpoints to the RLS HTTP fixture and assert a
second user receives 404 for a private article/annotation.

- [ ] **Step 3: Run and verify missing handler failures**

```bash
cd backend
go test ./internal/api -run 'Annotation|GuidedReading' -count=1
```

Expected: FAIL because `NewAnnotationHandler` and methods do not exist.

- [ ] **Step 4: Implement the focused handler and request types**

Create:

```go
type AnnotationHandler struct {
	articleRepo     *repository.ArticleRepository
	annotationRepo  *repository.AnnotationRepository
	templateRepo    *repository.TemplateRepository
	systemGenerator service.GuidedReadingGenerator
	cfg             *config.Config
}

func NewAnnotationHandler(
	articleRepo *repository.ArticleRepository,
	annotationRepo *repository.AnnotationRepository,
	templateRepo *repository.TemplateRepository,
	systemGenerator service.GuidedReadingGenerator,
	cfg *config.Config,
) *AnnotationHandler

type createAnnotationRequest struct {
	Kind         model.AnnotationKind `json:"kind"`
	Comment      string               `json:"comment"`
	BlockIndex   int                  `json:"block_index"`
	StartOffset  int                  `json:"start_offset"`
	EndOffset    int                  `json:"end_offset"`
	QuoteExact   string               `json:"quote_exact"`
	QuotePrefix  string               `json:"quote_prefix"`
	QuoteSuffix  string               `json:"quote_suffix"`
}

type updateAnnotationRequest struct {
	Kind    model.AnnotationKind `json:"kind"`
	Comment string               `json:"comment"`
}
```

Implement `GetSettings`, `PutSettings`, `List`, `Create`, `Update`, `Delete`,
and `Generate`. Every article method first calls
`articleRepo.WithCtx(c).GetByID(articleID, userID)` and maps invisibility to
404. At the start of every handler, bind
`annotationRepo := h.annotationRepo.WithCtx(c)` and use only that request-bound
repository for settings, annotation reads, mutations, and replacement. Create
reparses `article.Content`, calls `SelectorFromOffsets`,
verifies the exact/prefix/suffix against the canonical selector, computes the
fingerprint server-side, and persists `author_kind=user, origin_kind=user`.
Both create and update enforce: highlights have an empty comment;
evaluation/term comments trim to 1-600 runes; quotes trim to 1-400 runes; and
the kind is one of the three declared enum values.

Validation/status map:

```text
bad id/body/density/kind/comment       -> 400
article or annotation not visible      -> 404
guided reading disabled                -> 409
article content changed during AI call -> 409
empty/non-annotatable content          -> 422
no user or system AI key               -> 503
AI/model failure                       -> 502
successful create                      -> 201
successful delete/dismiss              -> 204
```

For `Generate`, load settings; select the user's AI configuration when present
with `h.templateRepo.WithCtx(c).GetUserAIConfig(userID)` and
`newUserSummarizer`; a configuration-query error returns 500 rather than
silently falling back. Call `service.GenerateGuidedReading`; then call
`ReplaceAIBatchIfContentUnchanged` with the article content captured before the
AI request. Return the freshly listed non-dismissed rows only after replacement
succeeds.

Choose the generator in this order: a non-empty per-user AI configuration,
then the injected system generator. If neither exists, return 503 before
starting orchestration. In `main.go`, keep the existing system summarizer for
the current summary features, but pass a separate
`service.GuidedReadingGenerator` variable to `NewAnnotationHandler`; assign it
from `summarizer` only when `cfg.Claude.APIKey != ""`. Handler tests inject a
fake generator directly and do not depend on machine configuration.

- [ ] **Step 5: Wire repository, handler, routes, and PATCH CORS**

In `main.go`:

```go
annotationRepo := repository.NewAnnotationRepository(db)
annotationHandler := api.NewAnnotationHandler(
	articleRepo, annotationRepo, templateRepo, summarizer, cfg,
)
```

Register:

```go
apiGroup.GET("/settings/ai-reading", annotationHandler.GetSettings)
apiGroup.PUT("/settings/ai-reading", annotationHandler.PutSettings)
apiGroup.GET("/articles/:id/annotations", annotationHandler.List)
apiGroup.POST("/articles/:id/annotations/generate", annotationHandler.Generate)
apiGroup.POST("/articles/:id/annotations", annotationHandler.Create)
apiGroup.PATCH("/articles/:id/annotations/:annotationId", annotationHandler.Update)
apiGroup.DELETE("/articles/:id/annotations/:annotationId", annotationHandler.Delete)
```

Add `PATCH` to `Access-Control-Allow-Methods`.

- [ ] **Step 6: Run handler, RLS, and full backend tests**

```bash
cd backend
go test ./internal/api ./internal/repository -run 'Annotation|GuidedReading|UserAIConfig|RLSHTTP' -count=1
go test ./... -count=1
```

Expected: both commands PASS.

- [ ] **Step 7: Commit the authenticated API**

```bash
git add backend/internal/api/annotation.go backend/internal/api/annotation_test.go \
  backend/internal/api/article.go backend/internal/api/article_summary_media_test.go \
  backend/internal/api/rls_http_leak_test.go backend/cmd/server/main.go
git commit -m "feat: expose guided reading annotation API"
```

## Task 7: Public share and Markdown export projection

**Files:**
- Modify: `backend/internal/api/share.go`
- Modify: `backend/internal/api/content.go`
- Modify: `backend/cmd/server/main.go`
- Create: `backend/internal/api/share_annotation_test.go`
- Create: `backend/internal/api/content_annotation_test.go`

- [ ] **Step 1: Write failing public/export tests**

The public test seeds four rows for the share owner: unchanged AI, edited AI
(`author_kind=user, origin_kind=ai`), direct user, and dismissed AI. Assert the
JSON contains article `content` and exactly one `ai_annotations` entry, and
does not serialize the owner's internal `user_id`.

The export test asserts exact section order and privacy:

```go
for _, want := range []string{
	"## AI 带读",
	"> 真正稀缺的是判断能力。",
	"- **观点评价：** 这是核心判断。",
	"**注意力经济**",
	"- **名词补充：** 一种分析框架。",
	"---\n\n## 正文",
} {
	if !strings.Contains(body, want) { t.Fatalf("missing %q\n%s", want, body) }
}
if strings.Contains(body, "我的私密评论") { t.Fatal("user note leaked") }
```

Also set `enabled=false` and assert both public annotations and the export
section disappear while the stored rows remain.

- [ ] **Step 2: Run and verify projection failures**

```bash
cd backend
go test ./internal/api -run 'ShareAnnotations|ExportAnnotations' -count=1
```

Expected: FAIL because share/export do not query annotations and share omits
content.

- [ ] **Step 3: Inject the annotation repository into both handlers**

Change constructors and startup wiring:

```go
func NewShareHandler(shareRepo *repository.ShareRepository, articleRepo *repository.ArticleRepository, annotationRepo *repository.AnnotationRepository) *ShareHandler
func NewContentHandler(articleRepo *repository.ArticleRepository, feedRepo *repository.FeedRepository, fetcher *rss.Fetcher, annotationRepo *repository.AnnotationRepository) *ContentHandler
```

Pass `annotationRepo` from `main.go`. Keep every query on `WithCtx(c)` so the
public-token transaction's owner is respected.

- [ ] **Step 4: Extend the share response**

After `GetArticleByToken`, call:

```go
annotations, err := h.annotationRepo.WithCtx(c).ListPublicAI(getUserID(c), article.ID)
if err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	return
}
```

Return `content` and `ai_annotations` alongside the existing fields. Never
accept an owner ID from the request; use the ID resolved by
`PublicTokenMiddleware`.

- [ ] **Step 5: Add the export formatter**

Implement a pure helper:

```go
func formatAIGuidedReading(items []model.ArticleAnnotation) string {
	if len(items) == 0 { return "" }
	var b strings.Builder
	b.WriteString("## AI 带读\n\n")
	for _, a := range items {
		switch a.Kind {
		case model.AnnotationHighlight:
			fmt.Fprintf(&b, "> %s\n\n", a.QuoteExact)
		case model.AnnotationEvaluation:
			fmt.Fprintf(&b, "> %s\n\n- **观点评价：** %s\n\n", a.QuoteExact, a.Comment)
		case model.AnnotationTerm:
			fmt.Fprintf(&b, "**%s**\n\n- **名词补充：** %s\n\n", a.QuoteExact, a.Comment)
		}
	}
	return b.String()
}
```

In `ExportMarkdown`, fetch `ListPublicAI(userID, article.ID)` and write this
section after summaries but before `---\n\n## 正文`. Propagate repository errors
instead of silently exporting an incomplete privacy projection.

- [ ] **Step 6: Run projection and full backend tests**

```bash
cd backend
go test ./internal/api -run 'ShareAnnotations|ExportAnnotations' -count=1
go test ./... -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit share/export behavior**

```bash
git add backend/internal/api/share.go backend/internal/api/content.go \
  backend/internal/api/share_annotation_test.go backend/internal/api/content_annotation_test.go \
  backend/cmd/server/main.go
git commit -m "feat: share and export AI reading notes"
```

## Task 8: Frontend test harness, API contract, and settings card

**Files:**
- Create: `frontend/vitest.config.ts`
- Create: `frontend/src/components/AIReadingSettingsCard.tsx`
- Create: `frontend/test/aiReadingSettings.test.tsx`
- Modify: `frontend/package.json`
- Modify: `frontend/package-lock.json`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/pages/SettingsPage.tsx`
- Modify: `frontend/test/readingProgress.test.ts`
- Modify: `frontend/test/popularFeedsVisibility.test.ts`

- [ ] **Step 1: Add a real frontend test runner**

Run:

```bash
cd frontend
npm install @noble/hashes@2.2.0
npm install --save-dev vitest@4.1.10 jsdom@29.1.1 @testing-library/react@16.3.2 @testing-library/user-event@14.6.1
```

Set the package script:

```json
"scripts": {
  "dev": "vite",
  "build": "tsc && vite build",
  "preview": "vite preview",
  "test": "vitest run"
}
```

Create `vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    include: ['test/**/*.test.{ts,tsx}'],
    clearMocks: true,
  },
})
```

- [ ] **Step 2: Add failing API/settings component tests**

Mock `getAIReadingSettings` and `saveAIReadingSettings`. Render the card and
assert:

```ts
expect((screen.getByRole('checkbox', { name: '启用 AI 带读' }) as HTMLInputElement).checked).toBe(true)
expect((screen.getByRole('radio', { name: /标准.*每千字 6 条/ }) as HTMLInputElement).checked).toBe(true)
expect((screen.getByRole('checkbox', { name: '生成观点评价' }) as HTMLInputElement).checked).toBe(true)
expect((screen.getByRole('checkbox', { name: '生成名词补充' }) as HTMLInputElement).checked).toBe(true)
```

Change density to dense, turn term off, click `保存带读设置`, and assert the
save call receives:

```ts
{
  enabled: true,
  density: 'dense',
  evaluation_enabled: true,
  term_enabled: false,
}
```

- [ ] **Step 3: Run and verify the red phase**

```bash
cd frontend
npm test -- --run test/aiReadingSettings.test.tsx
```

Expected: FAIL because the component and API methods do not exist.

Before the green run, convert `readingProgress.test.ts` and
`popularFeedsVisibility.test.ts` from top-level manual assertions into Vitest
`describe`/`it`/`expect` cases. Preserve every existing scenario and remove the
manual `console.log`; this prevents Vitest from reporting files with no test
suite while keeping the previous contracts active under `npm test`.

- [ ] **Step 4: Add TypeScript API types and calls**

Add to `client.ts`:

```ts
export type AnnotationKind = 'highlight' | 'evaluation' | 'term'
export type AnnotationAuthor = 'ai' | 'user'
export type AIReadingDensity = 'sparse' | 'standard' | 'dense'

export interface AIReadingSettings {
  enabled: boolean
  density: AIReadingDensity
  evaluation_enabled: boolean
  term_enabled: boolean
}

export interface ArticleAnnotation {
  id: number
  article_id: number
  kind: AnnotationKind
  comment: string
  author_kind: AnnotationAuthor
  origin_kind: AnnotationAuthor
  source_hash: string
  block_index: number
  start_offset: number
  end_offset: number
  quote_exact: string
  quote_prefix: string
  quote_suffix: string
  created_at: string
  updated_at: string
}

export interface CreateAnnotationInput {
  kind: AnnotationKind
  comment: string
  block_index: number
  start_offset: number
  end_offset: number
  quote_exact: string
  quote_prefix: string
  quote_suffix: string
}

export const getAIReadingSettings = () =>
  api.get<AIReadingSettings>('/settings/ai-reading').then(r => r.data)
export const saveAIReadingSettings = (settings: AIReadingSettings) =>
  api.put<AIReadingSettings>('/settings/ai-reading', settings).then(r => r.data)
export const getArticleAnnotations = (articleId: number) =>
  api.get<ArticleAnnotation[]>(`/articles/${articleId}/annotations`).then(r => r.data)
export const generateArticleAnnotations = (articleId: number) =>
  api.post<ArticleAnnotation[]>(`/articles/${articleId}/annotations/generate`, undefined, { timeout: 180000 }).then(r => r.data)
export const createArticleAnnotation = (articleId: number, input: CreateAnnotationInput) =>
  api.post<ArticleAnnotation>(`/articles/${articleId}/annotations`, input).then(r => r.data)
export const updateArticleAnnotation = (articleId: number, annotationId: number, input: Pick<CreateAnnotationInput, 'kind' | 'comment'>) =>
  api.patch<ArticleAnnotation>(`/articles/${articleId}/annotations/${annotationId}`, input).then(r => r.data)
export const deleteArticleAnnotation = (articleId: number, annotationId: number) =>
  api.delete(`/articles/${articleId}/annotations/${annotationId}`)
```

- [ ] **Step 5: Implement the focused settings card**

`AIReadingSettingsCard` owns loading, form state, save state, and inline error/
success messages. Render one master checkbox, a three-option radio group with
labels `稀疏（每千字约 3 条）`, `标准（每千字约 6 条）`, and
`密集（每千字约 10 条）`, plus the two comment checkboxes. Disable all controls
while saving. Mount it in the existing `{tab === 'ai'}` branch immediately
after `我的 AI 配置` and before `摘要模板`.

The save handler is exactly:

```ts
const save = async () => {
  setSaving(true)
  setMessage('')
  try {
    const saved = await saveAIReadingSettings(settings)
    setSettings(saved)
    setMessage('带读设置已保存')
  } catch {
    setMessage('保存失败，请重试')
  } finally {
    setSaving(false)
  }
}
```

- [ ] **Step 6: Run component tests and build**

```bash
cd frontend
npm test -- --run test/aiReadingSettings.test.tsx
npm run build
```

Expected: both PASS.

- [ ] **Step 7: Commit frontend settings foundation**

```bash
git add frontend/package.json frontend/package-lock.json frontend/vitest.config.ts \
  frontend/src/api/client.ts frontend/src/components/AIReadingSettingsCard.tsx \
  frontend/src/pages/SettingsPage.tsx frontend/test/aiReadingSettings.test.tsx \
  frontend/test/readingProgress.test.ts frontend/test/popularFeedsVisibility.test.ts
git commit -m "feat: configure AI guided reading"
```

## Task 9: Shared annotation resolver and responsive renderer

**Files:**
- Create: `frontend/src/annotations/types.ts`
- Create: `frontend/src/annotations/rehypeAnnotations.ts`
- Create: `frontend/src/annotations/AnnotationContext.tsx`
- Create: `frontend/src/components/AnnotationCard.tsx`
- Create: `frontend/src/components/AnnotationSidebar.tsx`
- Create: `frontend/test/annotationResolver.test.tsx`
- Create: `frontend/test/annotationCard.test.tsx`
- Modify: `frontend/src/components/MarkdownArticle.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write failing resolver/render tests**

Render `MarkdownArticle` with a short source and fixed annotations. Assert:

```ts
expect(container.querySelector('[data-annotation-id="11"]')?.className).toContain('annotation-anchor-evaluation')
expect(screen.getByText('这是核心判断。')).toBeTruthy()
expect(container.querySelector('[data-annotation-id="12"]')?.className).toContain('annotation-anchor-term')
expect(screen.getByText('一种分析框架。').closest('.annotation-card')?.className).toContain('annotation-card-term')
```

Add cases for:

- a plain highlight that decorates source text and creates no card;
- a quote split by `**inline emphasis**`;
- selectors inside both tight and loose Markdown list items, including a nested
  list, with block indices matching the backend test fixtures;
- source-hash mismatch with successful exact/context fallback;
- duplicate exact quotes with insufficient context, which render no anchor;
- cards sorted by `block_index/start_offset` regardless of API order;
- focusing or clicking a card scrolls its first source fragment and applies an
  active source class until focus leaves the card;
- read-only cards with no edit/delete buttons.

Read `src/index.css` in the renderer test and assert the term anchor uses a
dashed underline, the term card uses a dashed border, and the narrow layout is
guarded by `@media (max-width: 820px)`.

Mock `ResizeObserver` in the test file with a class whose `observe`,
`unobserve`, and `disconnect` methods are no-ops. Stub
`HTMLElement.prototype.scrollIntoView` and assert it is called by the card
focus test.

- [ ] **Step 2: Run and verify the red phase**

```bash
cd frontend
npm test -- --run test/annotationResolver.test.tsx test/annotationCard.test.tsx
```

Expected: FAIL because the renderer files and Markdown props do not exist.

- [ ] **Step 3: Define UI action/context contracts**

Create `types.ts`:

```ts
import type { ArticleAnnotation, AnnotationKind, CreateAnnotationInput } from '../api/client'

export interface AnnotationActions {
  create(input: CreateAnnotationInput): Promise<ArticleAnnotation>
  update(id: number, input: { kind: AnnotationKind; comment: string }): Promise<ArticleAnnotation>
  remove(id: number): Promise<void>
}

export interface AnnotationRenderOptions {
  annotations: ArticleAnnotation[]
  readOnly?: boolean
  actions?: AnnotationActions
  blockIndexOffset?: number
}
```

Create a context with `{ byId, readOnly, actions, focusSource, clearSourceFocus
}`, a provider, and a `useAnnotationContext()` hook. Build `byId` with
`useMemo` so card lookups are constant time. `focusSource(id)` removes the
previous active class, finds every matching source fragment inside the
annotation frame, adds `annotation-anchor-active` to all of them, and calls
`scrollIntoView({ block: 'center', behavior: 'smooth' })` on the first.

- [ ] **Step 4: Implement the rehype decoration plugin**

Expose:

```ts
export interface RehypeAnnotationOptions {
  annotations: ArticleAnnotation[]
  blockIndexOffset?: number
}

export function rehypeAnnotations(options: RehypeAnnotationOptions): (tree: HastRoot) => void
```

Use small local HAST interfaces instead of adding runtime traversal
dependencies. Recursively walk `p`, `h1`-`h6`, and tight-list `li` text blocks
in document order. A `li` becomes a block only for its direct inline children
when it has no direct paragraph child; exclude nested `ul`/`ol` descendants so
nested list items become separate blocks rather than duplicated text. Assign
`data-annotation-block`, flatten descendant text, normalize NBSP and whitespace,
and compute the current source hash with `sha256` from
`@noble/hashes/sha2.js` plus `bytesToHex`/`utf8ToBytes` from
`@noble/hashes/utils.js`. Hash the same normalized block strings joined with
`"\n\n"` as the backend. Resolve each selector in the approved order: use the
saved position only when hashes match and exact text also matches; then try the
saved block with prefix/suffix context; finally accept a global exact match
only when one context-scored best candidate remains.

Flatten each block into both normalized runes and a boundary map back to HAST
text-node/rune offsets. Collapse every cross-node whitespace run to one mapped
space, matching `strings.Fields` on the backend. Build all annotation boundary
events before mutating the tree, partition text into non-overlapping segments,
and decorate each segment with every covering annotation ID. Apply splits from
the end of each text node toward the start so earlier offsets stay valid. This
is what makes selections spanning inline emphasis and partially overlapping
annotations deterministic.

For every resolved range:

- split overlapping text nodes at rune-safe boundaries;
- wrap the overlapping fragments in `span` elements with
  `data-annotation-id` and one of `annotation-anchor-highlight`,
  `annotation-anchor-evaluation`, or `annotation-anchor-term`;
- insert one sibling `span data-annotation-slot="11,12"` after the owning block
  for narrow-screen cards; for a tight-list `li`, append the slot inside that
  `li` after its direct inline content and before any nested list so the DOM
  remains valid;
- attach `data-annotation-block` even when there are no annotations so the
  selection task can reuse the same block contract.

If multiple user annotations overlap, store all IDs in
`data-annotation-ids`; choose visual priority `term > evaluation > highlight`
without dropping any card. Single-ID fragments also carry
`data-annotation-id="<id>"` for simple inspection. Add one shared DOM helper
that finds fragments by exact single ID or by parsing the comma-separated
`data-annotation-ids`; use it for sidebar placement, card focus, and the stale
annotation count so a secondary overlapping annotation is never mistaken for
unresolved.

- [ ] **Step 5: Implement cards and measured sidebar**

`AnnotationCard` receives `annotation` and `readOnly`. Its label is derived
without free-form strings:

```ts
const owner = annotation.author_kind === 'ai' ? 'AI' : '我的'
const kindLabel = annotation.kind === 'evaluation' ? '观点评价'
  : annotation.kind === 'term' ? '名词补充' : '关键句'
```

Plain highlights do not create cards. Comment cards expose edit/delete only
when `readOnly !== true` and actions are present. Keep edit form state local;
Task 10 wires mutation behavior. Render the label as
`${owner} · ${kindLabel}`. Make each card focusable; focus/click calls
`focusSource(annotation.id)`, and blur calls `clearSourceFocus` unless focus is
still inside the same card.

`AnnotationSidebar` receives the frame root and sorted comment annotations. In
`useLayoutEffect`, use the shared DOM helper to find the first fragment for
each ID, calculate its top relative to the frame, and place cards in source
order. Enforce a 12px gap by moving each overlapping card down. Recalculate
through one `requestAnimationFrame` from a `ResizeObserver`.

- [ ] **Step 6: Extend MarkdownArticle without breaking existing contexts**

Add optional props:

```ts
type Props = {
  source: string
  imageDimensions?: Record<string, [number, number]>
  annotationOptions?: AnnotationRenderOptions
  urlTransform?: UrlTransform
}
```

Import `UrlTransform` from `react-markdown` as a type and pass the optional
function through to `ReactMarkdown`. The callback must remain optional so every
existing article keeps React Markdown's default URL sanitization.

Memoize the plugin tuple by `source`, annotation IDs/`updated_at`, and block
offset. Insert `rehypeAnnotations` before `rehypeHighlight` and `rehypeKatex`
so generated syntax-highlighting/KaTeX text cannot change the visible-block
contract. Add a `span` component override:

```tsx
span: ({ children, ...rest }) => {
  const slot = rest['data-annotation-slot'] as string | undefined
  if (slot) return <AnnotationInlineSlot ids={slot.split(',').map(Number)} />
  return <span {...rest}>{children}</span>
},
```

Wrap the existing `.markdown-body` in an annotation frame/provider only when
`annotationOptions` is present. Render `AnnotationSidebar` beside the source.
Keep the existing module-scoped image, link, paragraph, and code overrides, and
keep `memo(MarkdownArticle)` so scroll updates do not remount images.

- [ ] **Step 7: Add the confirmed responsive/accessibility styles**

Required CSS semantics:

```css
.annotation-anchor-highlight,
.annotation-anchor-evaluation { background: #fff0a8; }
.annotation-anchor-term {
  color: #137a4a;
  text-decoration-line: underline;
  text-decoration-style: dashed;
  text-decoration-color: #22a06b;
  text-underline-offset: 3px;
}
.annotation-card-evaluation {
  border-left: 3px solid #3b82f6;
  background: #eff6ff;
}
.annotation-card-term {
  border: 2px dashed #22a06b;
  background: #f0fdf6;
  color: #155e3b;
}
.annotation-anchor-active {
  outline: 2px solid currentColor;
  outline-offset: 2px;
}
```

At desktop width, use a source column plus a 280px relative sidebar and hide
inline slots. Below 820px, use one column, hide the sidebar, and display inline
slots after their blocks. Add keyboard-visible focus outlines and do not encode
kind by color alone.

- [ ] **Step 8: Run renderer tests and production build**

```bash
cd frontend
npm test -- --run test/annotationResolver.test.tsx test/annotationCard.test.tsx
npm run build
```

Expected: PASS, with no `noUnusedLocals` or React invalid-DOM warnings.

- [ ] **Step 9: Commit the shared renderer**

```bash
git add frontend/src/annotations frontend/src/components/AnnotationCard.tsx \
  frontend/src/components/AnnotationSidebar.tsx frontend/src/components/MarkdownArticle.tsx \
  frontend/src/index.css frontend/test/annotationResolver.test.tsx \
  frontend/test/annotationCard.test.tsx
git commit -m "feat: render guided reading annotations"
```

## Task 10: Selection toolbar, editor behavior, and annotation hook

**Files:**
- Create: `frontend/src/annotations/selection.ts`
- Create: `frontend/src/components/AnnotationSelectionToolbar.tsx`
- Create: `frontend/src/hooks/useArticleAnnotations.ts`
- Create: `frontend/test/annotationSelection.test.ts`
- Create: `frontend/test/annotationSelectionToolbar.test.tsx`
- Create: `frontend/test/useArticleAnnotations.test.tsx`
- Modify: `frontend/src/components/AnnotationCard.tsx`
- Modify: `frontend/src/components/MarkdownArticle.tsx`
- Modify: `frontend/test/annotationCard.test.tsx`
- Modify: `frontend/src/index.css`

- [ ] **Step 1: Write failing selection and ownership tests**

Build DOM fixtures carrying `data-annotation-block="0"`. Cover:

```ts
const input = selectionToAnnotationInput(root, selection, 'term', '解释')
expect(input).toMatchObject({
  kind: 'term',
  comment: '解释',
  block_index: 0,
  quote_exact: '注意力经济',
})
expect(input?.end_offset).toBeGreaterThan(input?.start_offset ?? 0)
```

Also assert `null` for a collapsed selection, cross-block range, selection
outside the root, and a range inside `pre, code`. Add a toolbar test that clears
the browser Selection after the toolbar opens, enters a comment, and still
creates the originally captured quote/offsets.

Extend card tests:

- opening/cancelling an AI card does not call update and keeps the `AI` label;
- saving calls update once and the wrapper rerenders with `author_kind=user`;
- delete calls `remove` and the card disappears only after success;
- rejected saves keep the editor and draft visible.

Use `renderHook` with mocked API promises to cover the hook before it exists:

- disabled settings hide AI-owned rows but retain user-owned rows;
- a generation rejection exposes an error and preserves the previous rows;
- rerendering from article A to article B, then resolving A last, leaves only
  B's state;
- create/update/remove change local rows only after their API promise resolves.

- [ ] **Step 2: Run and verify the red phase**

```bash
cd frontend
npm test -- --run test/annotationSelection.test.ts test/annotationSelectionToolbar.test.tsx test/annotationCard.test.tsx test/useArticleAnnotations.test.tsx
```

Expected: FAIL.

- [ ] **Step 3: Implement DOM Selection conversion**

Expose:

```ts
export function selectionToAnnotationInput(
  root: HTMLElement,
  selection: Selection,
  kind: AnnotationKind,
  comment: string,
): CreateAnnotationInput | null
```

Require both range endpoints to share one closest `[data-annotation-block]`.
Create a range from the block start to the selection start, normalize NBSP and
whitespace with the same contract as the rehype plugin, and count Unicode code
points with `Array.from`. Derive exact text, rune offsets, and 48-code-point
prefix/suffix. Reject exact text longer than 400 code points.

- [ ] **Step 4: Implement the article annotation hook**

The hook contract is:

```ts
export function useArticleAnnotations(articleId: number | null): {
  settings: AIReadingSettings | null
  annotations: ArticleAnnotation[]
  visibleAnnotations: ArticleAnnotation[]
  loading: boolean
  generating: boolean
  error: string
  actions: AnnotationActions
  generate(): Promise<void>
  reload(): Promise<void>
}
```

On article change, fetch settings and annotations with `Promise.all`. Visibility
is:

```ts
const visibleAnnotations = settings?.enabled === false
  ? annotations.filter(a => a.author_kind === 'user')
  : annotations
```

Generation uses the 180-second API override, replaces local rows with the
server response, and never clears existing rows on failure. Create appends and
sorts by block/start; update replaces by ID; remove deletes locally only after
the API succeeds. Guard load/generate completions with a monotonically
increasing request epoch (and an unmount flag) so a slow response for the prior
article cannot overwrite the new article's state. Use a ref guard inside
`generate()` as well as the button's disabled state to reject duplicate calls
from the same hook instance.

- [ ] **Step 5: Implement selection toolbar and card mutation UI**

On `mouseup`/keyboard selection inside editable Markdown, immediately call
`selectionToAnnotationInput(root, selection, 'highlight', '')` and store that
input-neutral selector snapshot before any toolbar button can move/collapse the
browser Selection. Position a toolbar at the captured range rectangle with
buttons `仅划线`, `观点评价`, `名词补充`. Highlight sends the stored input directly
and shows an undo-capable toast whose action calls
`actions.remove(created.id)`. Comment kinds open a compact textarea with Save/
Cancel; Save clones the stored selector, replaces `kind`/`comment`, and awaits
`actions.create`. Clear the snapshot only on cancel, successful save, an
outside click, Escape, or an article/source change.

In `AnnotationCard`, Save awaits `actions.update`; do not close the editor on a
rejection. Delete awaits `actions.remove`. The visible ownership badge always
comes from the current annotation prop, never from optimistic local text.

- [ ] **Step 6: Run interaction tests and build**

```bash
cd frontend
npm test -- --run test/annotationSelection.test.ts test/annotationSelectionToolbar.test.tsx test/annotationCard.test.tsx test/useArticleAnnotations.test.tsx
npm run build
```

Expected: PASS.

- [ ] **Step 7: Commit user annotation interactions**

```bash
git add frontend/src/annotations/selection.ts \
  frontend/src/components/AnnotationSelectionToolbar.tsx \
  frontend/src/components/AnnotationCard.tsx frontend/src/components/MarkdownArticle.tsx \
  frontend/src/hooks/useArticleAnnotations.ts frontend/src/index.css \
  frontend/test/annotationSelection.test.ts frontend/test/annotationSelectionToolbar.test.tsx \
  frontend/test/annotationCard.test.tsx frontend/test/useArticleAnnotations.test.tsx
git commit -m "feat: edit guided reading annotations"
```

## Task 11: Article page and immersive reading integration

**Files:**
- Create: `frontend/test/articleAnnotationsIntegration.test.tsx`
- Modify: `frontend/src/pages/ArticlePage.tsx`
- Modify: `frontend/src/components/ReadingLayout.tsx`
- Modify: `frontend/src/components/TweetCard.tsx`

- [ ] **Step 1: Write failing integration tests**

Add a `ReadingLayout` render test with one evaluation and assert the source
anchor and card both render. Add source-contract assertions for `ArticlePage`:

```ts
const articlePageSource = readFileSync(resolve('src/pages/ArticlePage.tsx'), 'utf8')
expect(articlePageSource).toContain('useArticleAnnotations')
expect(articlePageSource).toContain("生成带读")
expect(articlePageSource).toContain("重新生成")
expect(articlePageSource).toContain('annotationOptions={annotationOptions}')
expect(articlePageSource).toContain('annotations={annotationState.visibleAnnotations}')
```

Also assert that switching to `ReadingLayout` receives the same annotation
array and action object rather than making a second API request. Render a
`TweetCard` whose first Markdown block is the parsed byline and assert that an
annotation with `block_index: 1` resolves inside the tweet body while its
root-relative X link becomes `https://x.com/...`.

- [ ] **Step 2: Run and verify the red phase**

```bash
cd frontend
npm test -- --run test/articleAnnotationsIntegration.test.tsx
```

Expected: FAIL because the page and reading layout are not wired.

- [ ] **Step 3: Extend ReadingLayout props**

Add:

```ts
annotations: ArticleAnnotation[]
annotationActions: AnnotationActions
onUnresolvedAnnotations?: (ids: number[]) => void
```

Pass these to its existing `MarkdownArticle`:

```tsx
<MarkdownArticle
  source={article.content}
  annotationOptions={{ annotations, actions: annotationActions }}
  onUnresolvedAnnotations={onUnresolvedAnnotations}
/>
```

The immersive layout remains editable. It displays existing annotations but
does not add a second generation button.

- [ ] **Step 4: Load one annotation state in ArticlePage**

Near the other top-level hooks:

```ts
const parsedArticleId = id ? Number(id) : NaN
const articleId = Number.isInteger(parsedArticleId) && parsedArticleId > 0
  ? parsedArticleId
  : null
const annotationState = useArticleAnnotations(articleId)
const [staleAnnotationCount, setStaleAnnotationCount] = useState(0)
const annotationOptions = useMemo(() => ({
  annotations: annotationState.visibleAnnotations,
  actions: annotationState.actions,
}), [annotationState.visibleAnnotations, annotationState.actions])
```

Make `actions` stable inside the hook with `useMemo`; otherwise every reading-
progress render recreates the Markdown plugin tree.

Pass this one state to both the normal content `MarkdownArticle` and
`ReadingLayout`. Add an `onUnresolvedAnnotations` callback to `MarkdownArticle`:
after render, compare configured IDs with DOM anchor IDs and report the unresolved
IDs from an effect. Do not call state setters from the rehype transform. Define
the page callback with `useCallback((ids) => setStaleAnnotationCount(ids.length),
[])` and pass it to the normal renderer, `ReadingLayout`, and `TweetCard` so
every article kind/mode updates the same warning without an effect loop.

- [ ] **Step 5: Add the manual generation action and stale warning**

In the existing original-content title row, render only when settings are
loaded, enabled, and article content is non-empty:

```tsx
<button
  type="button"
  onClick={() => annotationState.generate()}
  disabled={annotationState.generating}
>
  {annotationState.generating
    ? 'AI 正在分析原文…'
    : annotationState.annotations.some(a => a.author_kind === 'ai')
      ? '重新生成'
      : '生成带读'}
</button>
```

Render API errors without clearing old annotations. When
`staleAnnotationCount > 0`, show `部分批注已失效（N 条），可重新生成` next to the
action. Existing user rows remain visible when the AI master switch is off.

Keep the generation action available for tweet details. Extend `TweetCard` with
optional `annotationOptions` and `onUnresolvedAnnotations` props and add
`blockIndexOffset` to the
`ParsedByline` result: return `1` when the byline regexp matched, otherwise
`0`. When annotation options are absent, retain the current compact/list
`ReactMarkdown` path exactly. When they are present, render `body` through
`MarkdownArticle` and pass:

```tsx
annotationOptions={{
  ...annotationOptions,
  blockIndexOffset: (annotationOptions.blockIndexOffset ?? 0) + blockIndexOffset,
}}
urlTransform={tweetURLTransform}
onUnresolvedAnnotations={onUnresolvedAnnotations}
```

Define the module-scoped `tweetURLTransform` with the `UrlTransform` type. It
prefixes only root-relative `href` values with `https://x.com`; all other
attributes go through `defaultUrlTransform`. This preserves X links without
rewriting relative image sources or weakening URL sanitization. Pass the same
`annotationOptions` from `ArticlePage` to its `TweetCard` detail branch. The
immersive reader continues to render the full stored Markdown, including the
byline, so it uses offset zero and resolves the same stored selectors.

- [ ] **Step 6: Run integration tests, the existing progress contract, and build**

```bash
cd frontend
npm test -- --run test/articleAnnotationsIntegration.test.tsx test/annotationResolver.test.tsx
node test/articleProgressBar.test.cjs
npm run build
```

Expected: PASS. The existing progress-bar static test must remain green because
annotation layout must not alter saved/current progress semantics.

- [ ] **Step 7: Commit reader integration**

```bash
git add frontend/src/pages/ArticlePage.tsx frontend/src/components/ReadingLayout.tsx \
  frontend/src/components/MarkdownArticle.tsx frontend/src/components/TweetCard.tsx \
  frontend/test/articleAnnotationsIntegration.test.tsx
git commit -m "feat: add AI guidance to article reading"
```

## Task 12: Public read-only annotation page

**Files:**
- Create: `frontend/test/shareAnnotations.test.tsx`
- Modify: `frontend/src/pages/SharePage.tsx`

- [ ] **Step 1: Write the failing public-page test**

Mock the public Axios response:

```ts
{
  title: '文章',
  url: 'https://example.com/article',
  content: '真正稀缺的是判断能力。',
  summary_brief: '',
  summary_detailed: '',
  published_at: null,
  ai_annotations: [{
    id: 1,
    article_id: 8,
    kind: 'evaluation',
    comment: '这是核心判断。',
    author_kind: 'ai',
    origin_kind: 'ai',
    source_hash: 'x',
    block_index: 0,
    start_offset: 0,
    end_offset: 12,
    quote_exact: '真正稀缺的是判断能力。',
    quote_prefix: '',
    quote_suffix: '',
    created_at: '2026-07-18T00:00:00Z',
    updated_at: '2026-07-18T00:00:00Z',
  }],
}
```

Render under `MemoryRouter` at `/share/token`. Assert the original body, blue
evaluation card, and `AI · 观点评价` label appear, while edit/delete/selection
controls do not.

- [ ] **Step 2: Run and verify the red phase**

```bash
cd frontend
npm test -- --run test/shareAnnotations.test.tsx
```

Expected: FAIL because `SharePage` ignores `content` and `ai_annotations`.

- [ ] **Step 3: Render the public content through the shared renderer**

Extend `SharedArticle`:

```ts
interface SharedArticle {
  title: string
  url: string
  content: string
  summary_brief: string
  summary_detailed: string
  published_at: string | null
  ai_annotations: ArticleAnnotation[]
}
```

Increase the outer max width to 1080px so the desktop sidebar fits. After the
summary card, add an original-content card containing:

```tsx
<MarkdownArticle
  source={article.content}
  annotationOptions={{ annotations: article.ai_annotations ?? [], readOnly: true }}
/>
```

Keep the existing original-link button and footer. The public page never
supplies mutation actions, so no private controls can render.

- [ ] **Step 4: Run public tests and build**

```bash
cd frontend
npm test -- --run test/shareAnnotations.test.tsx test/annotationResolver.test.tsx
npm run build
```

Expected: PASS.

- [ ] **Step 5: Commit public rendering**

```bash
git add frontend/src/pages/SharePage.tsx frontend/test/shareAnnotations.test.tsx
git commit -m "feat: show AI guidance on shared articles"
```

## Task 13: Full verification and documentation touch-up

**Files:**
- Modify: `README.md`
- Verify: all files changed in Tasks 1-12

- [ ] **Step 1: Document the user-visible feature**

Add one feature bullet near `AI 驱动总结`:

```markdown
- **AI 带读** — 手动生成关键句、观点评价和生僻名词补充；支持私人划线与评论、编辑接管和响应式侧栏
```

Do not add deployment claims or automatic-generation wording.

- [ ] **Step 2: Format and run all backend verification**

```bash
gofmt -w backend/internal/model/annotation.go \
  backend/internal/annotation/*.go backend/internal/ai/guided_reading*.go \
  backend/internal/service/guided_reading*.go backend/internal/repository/annotation*.go \
  backend/internal/api/annotation*.go backend/internal/api/share*.go \
  backend/internal/api/content*.go backend/cmd/server/main.go
cd backend
go test ./... -count=1
```

Expected: PASS with zero failed packages.

- [ ] **Step 3: Run all frontend tests, legacy contracts, and build**

```bash
cd frontend
npm test
node test/articleProgressBar.test.cjs
node test/popularFeedsSection.test.cjs
node test/favicon.test.cjs
npm run build
```

Expected: all test commands print their pass message or Vitest summary with zero
failures; TypeScript and Vite production build exit 0.

- [ ] **Step 4: Run database/privacy smoke checks**

With the local Postgres test service available:

```bash
cd backend
go test ./internal/repository -run 'TestMigration037|TestRLS_PrivateTablesAreScoped|AnnotationRepository' -count=1
go test ./internal/api -run 'Annotation|ShareAnnotations|ExportAnnotations|RLSHTTP' -count=1
```

Expected: PASS. Confirm the share/export tests prove user-owned content is
absent, not merely hidden in the frontend.

- [ ] **Step 5: Inspect the final diff and requirements matrix**

```bash
git diff --check
git status --short
git log --oneline --decorate -13
```

Confirm each approved requirement has evidence:

```text
manual generation                    -> generate handler + ArticlePage button
3/6/10 settings and max 30           -> TargetCount tests + settings card
AI/user creation                     -> service + selection toolbar
edit AI becomes private user content -> repository/API/card tests
regeneration preserves user content  -> atomic replacement tests
AI delete tombstone                  -> delete/regenerate tests
normal + immersive modes             -> integration test
desktop sidebar + mobile inline      -> renderer tests + CSS breakpoint
green dashed term semantics          -> component/CSS assertions
card focus locates source range      -> renderer interaction tests
tweet detail shares stored selectors -> TweetCard integration test
AI-only public share/export          -> backend projection tests
stale anchor fail-closed              -> resolver tests + UI warning
cross-user privacy                    -> RLS DB and HTTP tests
```

- [ ] **Step 6: Request code review, fix findings, and re-run verification**

Use `superpowers:requesting-code-review` against the complete branch. Address
all correctness/privacy findings, then repeat Steps 2-5 with fresh output.

- [ ] **Step 7: Commit final docs or verification-only adjustments**

If Step 6 changed files:

```bash
git add README.md backend frontend
git commit -m "docs: document AI guided reading"
```

If only `README.md` is uncommitted, stage and commit only that file. Do not
stage local database dumps, `.superpowers/`, build output, or unrelated user
files.
