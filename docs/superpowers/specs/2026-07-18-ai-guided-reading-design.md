# AI Guided Reading Annotations

Date: 2026-07-18
Status: Approved for implementation planning
Scope: article annotations, AI generation, reader rendering, settings, public sharing, and Markdown export

## Problem

RSS Pal currently offers article-level AI summaries, but readers still have to
identify the most important sentences and unfamiliar terms while moving through
the original text. The product needs an optional guided-reading layer that can:

- highlight important source sentences;
- attach an AI evaluation to a selected passage;
- explain an unfamiliar term beside the term itself;
- let the reader add the same kinds of annotations;
- let the reader edit AI output and take ownership of the edited annotation;
- preserve private user notes while allowing unchanged AI guidance to appear in
  public shares and Markdown exports.

The article body must remain the canonical source. Guided-reading data must not
rewrite or duplicate `articles.content`.

## Goals

- Generate guided-reading annotations only after the user explicitly clicks a
  button on an article.
- Store annotations as structured, per-user data with RLS enforcement.
- Render one annotation set in both the normal article page and immersive
  reading mode.
- Support user-created highlights, evaluations, and term explanations from a
  text-selection toolbar.
- Make ownership transitions explicit: saving an edit to an AI annotation
  turns it into user-owned content while retaining its AI origin for audit.
- Regenerate only annotations that are still AI-owned.
- Keep user-owned and dismissed content private.
- Keep annotations anchored when the article changes slightly, and fail closed
  when an anchor can no longer be resolved uniquely.

## Non-goals

- Automatic generation when a feed item is fetched or an article is opened.
- Collaborative comments or sharing user-written notes with other users.
- Cross-paragraph selections in the first version.
- A free-form guided-reading prompt editor or separate guided-reading model.
- Rewriting the stored Markdown to embed annotation syntax.
- Generating annotations for images that have no extractable or OCR-backed text.

## Confirmed Product Decisions

- Generation is manual. Existing results are cached until the user regenerates.
- AI and users can create annotations.
- Saving an edit to an AI annotation makes it user-owned. Merely opening or
  cancelling the editor does not.
- Settings include a master switch, density, evaluation enablement, and term
  explanation enablement.
- Density targets are approximately 3, 6, or 10 annotations per 1,000 visible
  text characters for sparse, standard, and dense modes.
- The same annotations appear in normal and immersive reading modes.
- Desktop uses an annotation sidebar; narrow screens place cards after their
  source text.
- Term explanations use a green dashed underline and a green dashed card
  border. Evaluations use a yellow source highlight and a blue card.
- Public shares and Markdown exports may include unchanged AI annotations but
  never user-owned content.
- Regeneration replaces only currently AI-owned annotations.

## Chosen Architecture

Keep article content and annotations separate. Each annotation uses a text quote
selector, modeled after the Web Annotation text-quote concept, plus positional
hints:

- exact selected text;
- normalized text immediately before and after the selection;
- block index and offsets inside the normalized block;
- a hash of the article text used when the selector was created.

The renderer resolves selectors against the Markdown AST and decorates only the
in-memory render tree. It never writes markers back into `articles.content`.

This approach supports per-user privacy, editing, regeneration, public
projection, and recovery after small article changes. A generated annotated
Markdown snapshot was rejected because it would make user privacy, source
refreshes, and partial regeneration brittle. Browser-only DOM paths were
rejected because they cannot support cross-device use or public sharing.

## Data Model

Migration `037_ai_guided_reading.sql` introduces two private tables.

### `ai_reading_settings`

One row per user:

| Column | Meaning |
| --- | --- |
| `user_id` | Primary key and owner |
| `enabled` | Master switch, default `true` |
| `density` | `sparse`, `standard`, or `dense`; default `standard` |
| `evaluation_enabled` | Generate evaluation comments; default `true` |
| `term_enabled` | Generate term explanations; default `true` |
| `created_at`, `updated_at` | Audit timestamps |

When no row exists, the GET endpoint returns these defaults without requiring a
write. The first settings update creates the row.

The three density values map to 3, 6, and 10 target annotations per 1,000
normalized visible characters. The final target is rounded, has a minimum of
one for non-empty content, and is capped at 30 AI annotations per article.

The master switch affects AI behavior only. When disabled:

- article pages hide the generate button and existing AI-owned annotations;
- generation requests are rejected;
- public shares and Markdown exports omit AI annotations;
- no annotation rows are deleted;
- user-owned annotations continue to render and remain editable.

### `article_annotations`

| Column | Meaning |
| --- | --- |
| `id` | Annotation identifier |
| `user_id` | Owner and RLS boundary |
| `article_id` | Annotated article |
| `kind` | `highlight`, `evaluation`, or `term` |
| `comment` | Empty for a plain highlight; required for comment types |
| `author_kind` | Current owner: `ai` or `user` |
| `origin_kind` | Immutable origin: `ai` or `user` |
| `generation_id` | AI batch identifier; null for direct user creation |
| `source_hash` | Hash of normalized article text at creation |
| `block_index` | Source block ordinal |
| `start_offset`, `end_offset` | Normalized offsets within the block |
| `quote_exact` | Exact selected source text |
| `quote_prefix`, `quote_suffix` | Bounded context used for re-anchoring |
| `fingerprint` | Stable normalized selector/type fingerprint |
| `dismissed` | User-owned tombstone for deleted AI-origin content |
| `created_at`, `updated_at` | Audit timestamps |

`highlight` has no comment. `evaluation` and `term` require a non-empty comment.
The API enforces bounded quote and comment lengths.

Indexes cover `(user_id, article_id, dismissed)`, `generation_id`, and
`fingerprint`. Both tables enable and force RLS using the repository's standard
`app_rls_bypass() OR user_id = app_current_user_id()` policy. Both tables are
added to the private-table RLS leak-test matrix.

### Ownership and deletion

- Creating an annotation from the selection toolbar writes
  `author_kind=user, origin_kind=user`.
- AI generation writes `author_kind=ai, origin_kind=ai`.
- Saving an edit to an AI annotation changes only `author_kind` to `user`.
- Deleting an AI-origin annotation creates or retains a dismissed, user-owned
  tombstone. Its fingerprint prevents an equivalent AI annotation from being
  reinserted by regeneration.
- Deleting a directly user-created annotation physically removes it.
- Dismissed rows never render, share, or export.

## Annotation Types and Visual Semantics

### Plain highlight

- Yellow source highlight.
- No comment card.
- Available to AI and users.

### Evaluation

- Yellow source highlight.
- Blue comment card labelled `AI · 观点评价` or `我的 · 观点评价`.
- Used for interpretation, critique, significance, or the role a passage plays
  in the author's argument.

### Term explanation

- Green dashed underline beneath the exact term.
- Pale green comment card with a green dashed border.
- Labelled `AI · 名词补充` or `我的 · 名词补充`.
- Used for concise definitions, background, abbreviations, or domain context.

The design never relies on color alone. Border style, underline style, visible
labels, and focus states distinguish annotation types.

## Reader Interaction

### Manual generation

The original-content header on `ArticlePage` gains a guided-reading action:

- `生成带读` when no visible AI batch exists;
- `重新生成` when AI annotations exist;
- an in-progress state that prevents a duplicate click in the same page;
- a failure message that leaves the previous AI batch untouched.

Immersive reading mode displays existing annotations but does not need a second
generation control. The shared data is refreshed when the user returns to or
reloads the article after generation.

### User creation

When the user selects text inside an editable article body, a compact toolbar
offers:

- `仅划线`;
- `观点评价`;
- `名词补充`.

Plain highlights save immediately and expose an undo toast. Comment types open
a compact editor before saving. A selection must be non-empty, remain within
one textual Markdown block, and stay within the configured quote-length limit.
Selections across paragraphs, code blocks, media, or non-text nodes are rejected
with an explanatory message.

### Desktop and narrow-screen layout

Desktop uses a two-column annotated article component:

- source content remains the primary column;
- cards in the side column are ordered by source position;
- each card aligns as closely as possible with its first source fragment;
- overlapping cards move downward without reordering;
- selecting or focusing a card scrolls to and emphasizes its source range.

A `ResizeObserver` and animation-frame measurement pass update card positions
when images or fonts change article height. The pass must not rewrite article
content or reading-progress state.

Below the responsive breakpoint, cards render in normal document flow after the
source block that owns the annotation. The mobile layout uses the same styles,
ownership labels, edit actions, and data as desktop.

### Editing

Opening an AI card editor leaves ownership unchanged. A successful save:

- validates the edited kind/comment;
- updates the row;
- changes `author_kind` to `user`;
- changes the visible badge from `AI` to `我的`;
- immediately excludes the annotation from future public projections.

## Source Anchoring and Rendering

The Markdown renderer is extended through a shared annotation layer rather than
duplicating logic in `ArticlePage` and `ReadingLayout`.

### Source preparation

The backend and frontend use the same normalization contract:

- preserve block order;
- derive visible plain text from paragraphs, headings, and list items;
- normalize line endings and repeated whitespace;
- do not include Markdown punctuation as visible text;
- assign deterministic block ordinals.

The backend computes selectors and fingerprints. The frontend consumes them.

### Resolver order

For each annotation, the renderer attempts:

1. position lookup when the source hash matches, followed by an exact-text
   assertion;
2. exact quote lookup inside the saved block, scored with prefix and suffix;
3. global exact quote lookup, accepted only when context produces one unique
   best match.

An unresolved or ambiguous selector is not rendered. The article page reports
the number of stale annotations and offers regeneration. User-owned stale rows
remain stored and are not silently deleted.

### React integration

A rehype annotation plugin resolves ranges and splits text nodes into decorated
fragments. It attaches annotation IDs and anchor markers to the output without
changing stored Markdown. A shared annotation context supplies cards, selection
actions, ownership, and editing callbacks.

The plugin configuration is memoized by article source and annotation revision.
Scroll-progress updates must not recreate the Markdown AST or remount lazy
images. This preserves the current `MarkdownArticle` performance contract.

## AI Generation

### Input

The backend converts the article into numbered visible-text blocks. Long
articles are split only at block boundaries into bounded prompt chunks. The
total target count is allocated proportionally across chunks and remains capped
at 30.

The prompt includes:

- article title;
- numbered source blocks;
- enabled annotation types;
- total target count and the chunk's allocation;
- Chinese output instructions;
- a strict requirement that every quote be copied verbatim from one block.

The existing per-user AI credentials and model take precedence. System AI is
used when the user has no personal AI configuration. No separate model or free-
form prompt setting is added.

### Output

The model returns structured JSON entries containing:

- block index;
- exact quote;
- kind;
- comment, when required.

The server computes offsets, context, source hash, generation ID, and
fingerprint. The model does not control ownership or database identifiers.

### Validation and atomic replacement

Before changing stored annotations, the server validates the complete batch:

- every block exists;
- every quote occurs in the declared block;
- the selected occurrence can be resolved uniquely;
- comment requirements and length limits pass;
- kinds are allowed by settings;
- duplicate fingerprints and heavily overlapping ranges are removed;
- the total result stays within the target and absolute cap.

If a prompt chunk fails, JSON cannot be parsed, or the final batch has no valid
entries, generation fails and the previous AI batch remains intact.

After validation, one database transaction:

1. loads preserved user-owned rows and dismissed AI-origin fingerprints;
2. deletes only rows whose current `author_kind` is `ai`;
3. filters new entries against tombstones;
4. inserts the new AI batch.

User-created rows, edited AI-origin rows, and dismissal tombstones are never
deleted by regeneration.

## API Design

All authenticated article routes verify that the current user can access the
article and use repositories bound through `WithCtx(c)`.

### Settings

- `GET /api/settings/ai-reading`
- `PUT /api/settings/ai-reading`

The PUT endpoint accepts only the master switch, density enum, and the two
comment-type switches. It returns the normalized saved settings.

### Article annotations

- `GET /api/articles/:id/annotations`
- `POST /api/articles/:id/annotations/generate`
- `POST /api/articles/:id/annotations`
- `PATCH /api/articles/:id/annotations/:annotationId`
- `DELETE /api/articles/:id/annotations/:annotationId`

The authenticated GET response returns all non-dismissed annotations owned by
the current user. It still returns AI-owned rows while the master switch is off,
so re-enabling the setting restores them without regeneration; the authenticated
UI applies the switch's visibility rule. Mutation endpoints reject annotation
IDs owned by another user even if the article itself is shared across users.

Generation returns the validated batch after it has been committed. The first
version uses one request rather than adding a durable job queue; the UI shows an
indeterminate analysis state and can retry safely because replacement is
atomic.

### Public share projection

The existing share-token owner is the annotation owner for the public request.
The public article payload gains an `ai_annotations` collection selected with:

- matching article and share owner;
- `author_kind=ai`;
- `dismissed=false`;
- guided reading currently enabled for the owner.

Public share pages are read-only. User-owned rows are excluded even when their
`origin_kind` is `ai`.

## Markdown Export

Markdown export keeps the original article body unchanged. When guided reading
is enabled and visible AI annotations exist, it adds an `## AI 带读` section
before the original content. Entries appear in source order:

- plain highlights as quoted key sentences;
- evaluations as a quoted sentence followed by an `观点评价` bullet;
- terms as a bold term followed by a `名词补充` bullet.

Export applies the same AI-visibility predicate as public sharing, with the
authenticated current user as owner. User-owned and dismissed content is never
included.

## Error Handling

- Empty or non-textual article content returns a validation error without
  modifying annotations.
- Disabled guided reading rejects generation and hides AI output without
  deleting it.
- AI timeout, malformed output, or a failed prompt chunk preserves the previous
  batch.
- A partially invalid model response is filtered; zero valid entries fails the
  batch.
- Concurrent duplicate clicks are prevented in the page. If requests still
  race across tabs, each transaction remains internally atomic and the last
  successfully committed batch becomes current.
- Stale selectors are counted and hidden rather than attached to a guessed
  sentence.
- Frontend mutation failures keep the editor open and preserve its draft.

## Components and Code Boundaries

Expected backend boundaries:

- `model`: annotation and settings types;
- `repository`: RLS-scoped CRUD, generation replacement, public projection;
- `ai` or `service`: block preparation, prompt construction, response parsing,
  selector validation, and generation orchestration;
- `api`: settings and article annotation handlers;
- share/export handlers: consume the public AI projection.

Expected frontend boundaries:

- API types and calls in `frontend/src/api/client.ts`;
- a shared annotated Markdown renderer built around `MarkdownArticle`;
- a selector resolver and rehype decoration plugin with pure unit tests;
- sidebar/inline card components;
- selection toolbar and editor components;
- settings controls inside the existing AI tab;
- read-only integration in the public share page.

Each unit has one primary responsibility. `ArticlePage` coordinates loading and
actions but does not implement selector matching or card layout internally.

## Testing

### Backend

- Migration smoke test and table constraints.
- RLS leak tests for settings and annotations.
- Repository tests for per-user CRUD and article access.
- Ownership transition tests for editing AI annotations.
- Tombstone tests proving regeneration does not restore an equivalent dismissed
  AI annotation.
- Generation validation tests for fabricated quotes, duplicate quotes, invalid
  kinds, missing comments, overlap, cap enforcement, and malformed JSON.
- Atomic replacement tests proving a failed generation preserves the old batch
  and a successful generation preserves all user-owned rows.
- Re-anchoring tests for unchanged, lightly edited, ambiguous, and missing
  source text.
- Public share and Markdown export tests proving only enabled, non-dismissed,
  currently AI-owned rows are exposed.

### Frontend

- Selector resolver tests across paragraphs, headings, list items, and inline
  Markdown formatting.
- Component tests for all three annotation styles and ownership badges.
- Selection toolbar validation for single-block and rejected cross-block ranges.
- Edit tests proving ownership changes only after a successful save.
- Desktop side-card ordering and narrow-screen inline-card behavior.
- Shared rendering tests for normal and immersive reading modes.
- Settings tests for defaults, density mapping, toggles, and master-switch
  visibility.
- Read-only public-share rendering tests.
- Production build verification with `npm run build`.
- Full backend verification with `go test ./...`.

## Acceptance Criteria

- A user can manually generate guided reading for a text article and reload the
  page without another AI call.
- Sparse, standard, and dense modes target 3, 6, and 10 annotations per 1,000
  visible characters, capped at 30.
- Evaluations and term explanations can be independently disabled.
- Users can select one block of text and create any of the three annotation
  types.
- Both normal and immersive modes show the same resolved annotations.
- Desktop and narrow-screen layouts match the confirmed side-card and inline-
  card design.
- Term annotations use a green dashed underline and green dashed card border.
- Editing an AI annotation makes it private and protects it from regeneration.
- Deleting an AI-origin annotation prevents an equivalent regeneration result
  from reappearing.
- Regeneration never removes user-created, user-edited, or dismissed content.
- Public shares and Markdown exports expose unchanged AI annotations only.
- An article refresh never attaches an annotation to an ambiguous or unrelated
  passage.
- RLS tests prove cross-user annotation isolation.
