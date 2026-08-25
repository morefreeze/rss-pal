# Interest Rename Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename the application-level insight feature to interest, make `/interests` and `/api/interests/*` canonical, remove the recommendation page from routing/navigation without deleting its implementation, and preserve one-release compatibility for old interest URLs and API clients.

**Architecture:** The frontend receives a canonical `InterestsPage`, interest-named client types/functions, and a testable exported route tree. The backend receives interest-named model, repository, AI, handler, and worker code while the persistence layer continues to use the existing `user_insights` table. Legacy UI/API paths are thin adapters around canonical interest behavior; recommendation code remains untouched but unregistered from the UI router.

**Tech Stack:** React 18, React Router 6, TypeScript, Vitest, Testing Library, Go 1.24, Gin, PostgreSQL.

---

## File map

### Frontend

- Create `frontend/src/pages/InterestsPage.tsx`: canonical interest page.
- Delete `frontend/src/pages/InsightsPage.tsx`: superseded code-level name.
- Modify `frontend/src/App.tsx`: export the authenticated route tree, register `/interests`, redirect `/insights`, and stop registering `/recommended`.
- Modify `frontend/src/api/client.ts`: interest-named types/functions and canonical API URLs.
- Modify `frontend/src/components/Layout.tsx`: desktop navigation.
- Modify `frontend/src/components/MoreSheet.tsx`: mobile overflow navigation.
- Modify `frontend/src/components/RecommendationsCard.tsx`: return-navigation path.
- Modify `frontend/src/utils/pageTitle.ts`: canonical interest title and legacy title during redirect.
- Modify `frontend/test/pageTitle.test.ts`: route-title regression.
- Create `frontend/test/InterestNavigation.test.tsx`: desktop/mobile navigation regression.
- Create `frontend/test/InterestRoutes.test.tsx`: canonical, legacy, and removed-route behavior.

### Backend core

- Modify `backend/internal/model/model.go`: `UserInterest` and `InterestCandidate`.
- Create `backend/internal/repository/interest.go`; delete `backend/internal/repository/insight.go`: code-level repository rename while SQL keeps `user_insights`.
- Modify `backend/internal/repository/article.go`: `GetInterestCandidates`.
- Create `backend/internal/ai/interest_prompt.go`; delete `backend/internal/ai/insight_prompt.go`.
- Create `backend/internal/ai/interest_parse.go`; delete `backend/internal/ai/insight_parse.go`.
- Rename matching AI tests to `interest_prompt_test.go` and `interest_parse_test.go`.
- Modify `backend/internal/ai/summarizer.go`: interest-named generation methods.

### Backend API and worker

- Create `backend/internal/api/interests.go`; delete `backend/internal/api/insights.go`: canonical handler and legacy response adapter.
- Create `backend/cmd/server/interest_routes.go`: canonical plus compatibility route registration.
- Create `backend/cmd/server/interest_routes_test.go`: route table regression.
- Modify `backend/cmd/server/main.go`: interest repository/handler wiring.
- Create `backend/cmd/worker/interests.go`; delete `backend/cmd/worker/insights.go`.
- Create `backend/cmd/worker/interests_test.go`; delete `backend/cmd/worker/insights_test.go`.
- Modify `backend/cmd/worker/main.go` and `backend/cmd/worker/briefing.go`: interest scheduler wiring and terminology.

Historical migrations and their `user_insights` identifiers are explicitly excluded.

---

### Task 1: Frontend interest routes, navigation, page, and client

**Files:**
- Create: `frontend/test/InterestNavigation.test.tsx`
- Create: `frontend/test/InterestRoutes.test.tsx`
- Modify: `frontend/test/pageTitle.test.ts`
- Create: `frontend/src/pages/InterestsPage.tsx`
- Delete: `frontend/src/pages/InsightsPage.tsx`
- Modify: `frontend/src/App.tsx`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/components/Layout.tsx`
- Modify: `frontend/src/components/MoreSheet.tsx`
- Modify: `frontend/src/components/RecommendationsCard.tsx`
- Modify: `frontend/src/utils/pageTitle.ts`

- [ ] **Step 1: Add the failing page-title assertion**

Append this test inside `frontend/test/pageTitle.test.ts`:

```ts
it('names the canonical interest page', () => {
  expect(getRoutePageTitle('/interests')).toBe('兴趣')
  expect(getRoutePageTitle('/insights')).toBe('兴趣')
})
```

- [ ] **Step 2: Add failing desktop and mobile navigation tests**

Create `frontend/test/InterestNavigation.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import Layout from '../src/components/Layout'
import MoreSheet from '../src/components/MoreSheet'

vi.mock('../src/api/client', () => ({
  getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test' }),
  getUnreadCount: vi.fn().mockResolvedValue(0),
}))

describe('interest navigation', () => {
  it('exposes Interest and removes Recommended from desktop navigation', async () => {
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <Routes>
          <Route element={<Layout user={{ id: 1, username: 'reader', is_admin: false }} onLogout={() => {}} />}>
            <Route path="/articles" element={<div>Articles</div>} />
          </Route>
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('link', { name: /兴趣/ })).toHaveAttribute('href', '/interests')
    expect(screen.queryByRole('link', { name: /推荐/ })).not.toBeInTheDocument()
  })

  it('exposes Interest and removes Recommended from the mobile sheet', () => {
    render(
      <MemoryRouter>
        <MoreSheet open onClose={() => {}} onLogout={() => {}} />
      </MemoryRouter>,
    )

    expect(screen.getByRole('button', { name: /兴趣/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /推荐/ })).not.toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Add failing route behavior tests**

Create `frontend/test/InterestRoutes.test.tsx`:

```tsx
import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { AppRoutes } from '../src/App'

vi.mock('../src/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../src/api/client')>()
  return {
    ...actual,
    isLoggedIn: () => true,
    getUnreadCount: vi.fn().mockResolvedValue(0),
    getServerHealth: vi.fn().mockResolvedValue({ status: 'ok', version: 'test' }),
    getTopics: vi.fn().mockResolvedValue([]),
    getTags: vi.fn().mockResolvedValue([]),
    getLatestInterests: vi.fn().mockResolvedValue({
      interest: null,
      remaining_today: 3,
      remaining_month: 100,
    }),
  }
})

vi.mock('../src/pages/ArticleListPage', () => ({ default: () => <div>Articles</div> }))

function LocationProbe() {
  return <output data-testid="pathname">{useLocation().pathname}</output>
}

function renderAt(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <AppRoutes
        user={{ id: 1, username: 'reader', is_admin: false }}
        onLogin={() => {}}
        onLogout={() => {}}
      />
      <LocationProbe />
    </MemoryRouter>,
  )
}

describe('interest routes', () => {
  it('serves the canonical interest route', async () => {
    renderAt('/interests')
    await waitFor(() => expect(screen.getByTestId('pathname')).toHaveTextContent('/interests'))
  })

  it('redirects the legacy insight route to interests', async () => {
    renderAt('/insights')
    await waitFor(() => expect(screen.getByTestId('pathname')).toHaveTextContent('/interests'))
  })

  it('no longer registers the recommended route', async () => {
    renderAt('/recommended')
    await waitFor(() => expect(screen.getByTestId('pathname')).toHaveTextContent('/articles'))
  })
})
```

- [ ] **Step 4: Run the focused frontend tests and verify RED**

Run:

```bash
cd frontend
npx vitest run test/pageTitle.test.ts test/InterestNavigation.test.tsx test/InterestRoutes.test.tsx
```

Expected: FAIL because `/interests`, interest navigation, and `AppRoutes` do not exist yet and `/recommended` is still registered.

- [ ] **Step 5: Implement canonical interest frontend names and routes**

Apply these exact public mappings:

```ts
// frontend/src/api/client.ts
export interface PersistedInterest {
  id: number
  content: string
  status: 'pending' | 'done' | 'failed'
  error_msg?: string
  triggered_by: 'auto' | 'manual'
  model?: string
  generated_at: string
  recommendations?: RecommendationDirection[]
}

export interface InterestsLatest {
  interest: PersistedInterest | null
  remaining_today: number
  remaining_month: number
  rec_articles?: Record<string, RecArticleMeta>
}

export const getLatestInterests = () =>
  api.get<InterestsLatest>('/interests/latest').then(res => res.data)

export const generateInterests = () =>
  api.post<GenerateInterestsResp>('/interests/generate').then(res => res.data)
```

Rename `GenerateInsightsResp` to `GenerateInterestsResp`, update its comments,
and create `frontend/src/pages/InterestsPage.tsx` from the existing page with:

```tsx
export default function InterestsPage() {
  // Preserve behavior, but use interest/setInterest, PersistedInterest,
  // getLatestInterests, and generateInterests throughout.
}
```

Use these user-facing strings:

```text
兴趣
AI 个性化兴趣分析
点击右上角生成兴趣分析
还没有足够数据形成兴趣画像
兴趣画像基于你对文章的反应形成。试着：
```

Refactor `frontend/src/App.tsx` so the router is testable and canonical:

```tsx
export function AppRoutes({ user, onLogin, onLogout }: {
  user: User | null
  onLogin: (user: User) => void
  onLogout: () => void
}) {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage onLogin={onLogin} />} />
      <Route path="/register" element={<RegisterPage onLogin={onLogin} />} />
      <Route path="/share/:token" element={<SharePage />} />
      <Route path="/extension-config" element={<ExtensionConfigPage />} />
      <Route element={<RequireAuth user={user} onLogout={onLogout} />}>
        <Route index element={<Navigate to="/articles" replace />} />
        <Route path="feeds" element={<FeedListPage />} />
        <Route path="feeds/health" element={<FeedHealthPage />} />
        <Route path="briefing" element={<BriefingRedirect />} />
        <Route path="daily" element={<DailyPage />} />
        <Route path="weekly" element={<WeeklyPage />} />
        <Route path="articles" element={<ArticleListPage />} />
        <Route path="articles/:id" element={<ArticlePage />} />
        <Route path="clip" element={<ClipPage />} />
        <Route path="saved" element={<Navigate to="/articles?saved=1" replace />} />
        <Route path="interests" element={<InterestsPage />} />
        <Route path="insights" element={<Navigate to="/interests" replace />} />
        <Route path="stats" element={<StatsPage />} />
        <Route path="settings" element={<SettingsPage user={user} />} />
      </Route>
      <Route path="*" element={<Navigate to="/articles" replace />} />
    </Routes>
  )
}
```

Update the route test calls to pass `onLogin={() => {}}`.

`App` keeps `BrowserRouter` and `RoutePageTitle`, then renders `<AppRoutes
user={user} onLogin={setUser} onLogout={handleLogout} />`. Remove the
`RecommendedPage` import and route but do not modify or delete
`RecommendedPage.tsx`.

Apply these navigation/title mappings:

```ts
// Layout.tsx / MoreSheet.tsx
{ to: '/interests', icon: '💡', label: '兴趣' }

// pageTitle.ts
'/interests': '兴趣',
'/insights': '兴趣',
```

Remove the `/recommended` title entry. In `RecommendationsCard.tsx`, replace
both stored/route origin paths with `/interests`.

- [ ] **Step 6: Run focused tests and verify GREEN**

Run:

```bash
cd frontend
npx vitest run test/pageTitle.test.ts test/InterestNavigation.test.tsx test/InterestRoutes.test.tsx
```

Expected: all focused tests PASS.

- [ ] **Step 7: Commit the frontend slice**

```bash
git add frontend/src frontend/test/pageTitle.test.ts frontend/test/InterestNavigation.test.tsx frontend/test/InterestRoutes.test.tsx
git commit -m "feat(frontend): rename insights to interests"
```

---

### Task 2: Backend interest model, repository, AI prompt, and parser

**Files:**
- Modify: `backend/internal/model/model.go`
- Create: `backend/internal/repository/interest.go`
- Delete: `backend/internal/repository/insight.go`
- Modify: `backend/internal/repository/article.go`
- Create: `backend/internal/ai/interest_prompt.go`
- Delete: `backend/internal/ai/insight_prompt.go`
- Create: `backend/internal/ai/interest_parse.go`
- Delete: `backend/internal/ai/insight_parse.go`
- Create: `backend/internal/ai/interest_prompt_test.go`
- Delete: `backend/internal/ai/insight_prompt_test.go`
- Create: `backend/internal/ai/interest_parse_test.go`
- Delete: `backend/internal/ai/insight_parse_test.go`
- Modify: `backend/internal/ai/summarizer.go`
- Modify: `backend/internal/api/insights.go` (temporary consumer update; renamed in Task 3)
- Modify: `backend/cmd/server/main.go` (repository constructor consumer)
- Modify: `backend/cmd/worker/insights.go` (temporary consumer update; renamed in Task 4)
- Modify: `backend/cmd/worker/main.go` (repository constructor consumer)

- [ ] **Step 1: Rename test contracts before implementation**

Move the existing prompt/parser test content to the interest-named files and
apply these symbol mappings only in tests:

```text
InsightCandidate       -> InterestCandidate
BuildInsightPrompt     -> BuildInterestPrompt
ParseInsightJSON       -> ParseInterestJSON
TestBuildInsightPromptCandidatesIncludeIDsAndReadMarker -> TestBuildInterestPromptCandidatesIncludeIDsAndReadMarker
TestBuildInsightPromptEmptyCandidatesStillProducesPrompt -> TestBuildInterestPromptEmptyCandidatesStillProducesPrompt
TestParseInsightJSON_HappyPath -> TestParseInterestJSON_HappyPath
TestParseInsightJSON_FenceWrapped -> TestParseInterestJSON_FenceWrapped
TestParseInsightJSON_DropsInvalidIDsAndKinds -> TestParseInterestJSON_DropsInvalidIDsAndKinds
TestParseInsightJSON_TotalGarbage -> TestParseInterestJSON_TotalGarbage
TestParseInsightJSON_CapsAt3DirectionsAnd5Articles -> TestParseInterestJSON_CapsAt3DirectionsAnd5Articles
```

- [ ] **Step 2: Run focused backend tests and verify RED**

Run:

```bash
cd backend
go test ./internal/ai ./internal/repository -count=1
```

Expected: build FAIL with undefined `model.InterestCandidate`,
`BuildInterestPrompt`, and `ParseInterestJSON`.

- [ ] **Step 3: Rename backend core symbols without changing behavior**

Apply this complete symbol/file mapping:

```text
model.UserInsight                    -> model.UserInterest
model.InsightCandidate               -> model.InterestCandidate
UserInsightRepository                -> UserInterestRepository
NewUserInsightRepository             -> NewUserInterestRepository
GetInsightCandidates                 -> GetInterestCandidates
BuildInsightPrompt                   -> BuildInterestPrompt
ParseInsightJSON                     -> ParseInterestJSON
insightEnvelope                      -> interestEnvelope
GenerateUserInsight                  -> GenerateUserInterest
GenerateUserInsightJSON              -> GenerateUserInterestJSON
GenerateInsights                     -> GenerateInterests
internal/repository/insight.go        -> internal/repository/interest.go
internal/ai/insight_prompt.go         -> internal/ai/interest_prompt.go
internal/ai/insight_parse.go          -> internal/ai/interest_parse.go
```

Every SQL statement copied into `interest.go` must retain the literal table
identifier `user_insights`; only Go filenames, types, receivers, variables, and
comments change.

Rename local variables such as `ui` to `interest` where they represent the
domain object. Update the still-insight-named API and worker files to call the
new repository/model/AI symbols so the repository rename does not leave the
branch uncompilable. Do not yet rename their handler/scheduler types; Tasks 3
and 4 own that terminology. Do not change scoring, caps, prompt JSON schema,
quotas, or SQL.

- [ ] **Step 4: Format and run focused backend tests GREEN**

Run:

```bash
cd backend
gofmt -w internal/model/model.go internal/repository/article.go internal/repository/interest.go internal/ai/interest_*.go internal/ai/summarizer.go internal/api/insights.go cmd/server/main.go cmd/worker/insights.go cmd/worker/main.go
go test ./... -count=1
```

Expected: the complete backend suite PASS, proving all intermediate consumers
compile against the renamed core.

- [ ] **Step 5: Commit the backend core slice**

```bash
git add backend/internal/model backend/internal/repository backend/internal/ai backend/internal/api/insights.go backend/cmd/server/main.go backend/cmd/worker/insights.go backend/cmd/worker/main.go
git commit -m "refactor(backend): rename insight core to interest"
```

---

### Task 3: Canonical interest API with legacy compatibility

**Files:**
- Create: `backend/internal/api/interests.go`
- Delete: `backend/internal/api/insights.go`
- Create: `backend/internal/api/interests_test.go`
- Create: `backend/cmd/server/interest_routes.go`
- Create: `backend/cmd/server/interest_routes_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Add the failing server route-table test**

Create `backend/cmd/server/interest_routes_test.go`:

```go
package main

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func TestRegisterInterestRoutesIncludesCanonicalAndLegacyPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerInterestRoutes(router.Group("/api"), &api.InterestsHandler{})

	got := map[string]bool{}
	for _, route := range router.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"GET /api/interests/latest",
		"POST /api/interests/generate",
		"GET /api/insights/latest",
		"POST /api/insights/generate",
	} {
		if !got[want] {
			t.Errorf("missing route %s", want)
		}
	}
}
```

Create `backend/internal/api/interests_test.go`:

```go
package api

import (
	"testing"

	"github.com/bytedance/rss-pal/internal/model"
)

func TestNewInterestLatestResponseUsesRequestedPayloadKey(t *testing.T) {
	interest := &model.UserInterest{ID: 7}
	quota := interestQuota{RemainingToday: 2, RemainingMonth: 99}

	canonical := newInterestLatestResponse("interest", interest, quota)
	if canonical["interest"] != interest || canonical["insight"] != nil {
		t.Fatalf("canonical response keys = %#v", canonical)
	}

	legacy := newInterestLatestResponse("insight", interest, quota)
	if legacy["insight"] != interest || legacy["interest"] != nil {
		t.Fatalf("legacy response keys = %#v", legacy)
	}
}
```

- [ ] **Step 2: Run the route test and verify RED**

Run:

```bash
cd backend
go test ./cmd/server ./internal/api -run 'TestRegisterInterestRoutesIncludesCanonicalAndLegacyPaths|TestNewInterestLatestResponseUsesRequestedPayloadKey' -count=1
```

Expected: build FAIL because `registerInterestRoutes`, `api.InterestsHandler`,
`interestQuota`, and `newInterestLatestResponse` do not exist yet.

- [ ] **Step 3: Implement interest handler and response compatibility**

Rename the handler and dependencies:

```go
type InterestsHandler struct {
	prefRepo         *repository.PreferenceRepository
	articleRepo      *repository.ArticleRepository
	templateRepo     *repository.TemplateRepository
	userInterestsRepo *repository.UserInterestRepository
	summarizer       *ai.Summarizer
	cfg              *config.Config
}
```

The canonical and legacy latest methods share one implementation:

```go
func (h *InterestsHandler) Latest(c *gin.Context) {
	h.latest(c, "interest")
}

func (h *InterestsHandler) LatestLegacy(c *gin.Context) {
	h.latest(c, "insight")
}

func (h *InterestsHandler) latest(c *gin.Context, payloadKey string) {
	userID := getUserID(c)
	interest, _ := h.userInterestsRepo.WithCtx(c).GetLatest(userID)
	quota, _ := h.computeQuota(c, userID)
	resp := newInterestLatestResponse(payloadKey, interest, quota)
	// recommendation metadata enrichment follows below
```

Add the tested response constructor:

```go
func newInterestLatestResponse(payloadKey string, interest *model.UserInterest, quota interestQuota) gin.H {
	return gin.H{
		payloadKey:       interest,
		"remaining_today": quota.RemainingToday,
		"remaining_month": quota.RemainingMonth,
	}
}
```

Then complete `latest` with the existing enrichment expressed in interest
terminology:

```go
	if interest != nil && len(interest.Recommendations) > 0 {
		ids := make([]int, 0)
		seen := map[int]bool{}
		for _, direction := range interest.Recommendations {
			for _, article := range direction.Articles {
				if !seen[article.ArticleID] {
					seen[article.ArticleID] = true
					ids = append(ids, article.ArticleID)
				}
			}
		}
		if len(ids) > 0 {
			articles, err := h.articleRepo.WithCtx(c).GetByIDsForUser(userID, ids)
			if err != nil {
				log.Printf("interests: Latest GetByIDsForUser user=%d: %v", userID, err)
			} else {
				meta := make(map[string]gin.H, len(articles))
				for _, article := range articles {
					brief := []rune(article.SummaryBrief)
					if len(brief) > 80 {
						brief = brief[:80]
					}
					meta[strconv.Itoa(article.ID)] = gin.H{
						"id":         article.ID,
						"title":      article.Title,
						"feed_title": article.FeedTitle,
						"brief":      string(brief),
						"is_read":    article.IsRead,
					}
				}
				resp["rec_articles"] = meta
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}
```

Rename `insightQuota` to `interestQuota`, handler receiver/type names, local
variables, logs (`interests:`), AI calls, and candidate types. Keep HTTP
statuses, quota behavior, async behavior, and JSON recommendation schema.
Change the no-data message to `暂无足够的阅读数据来生成兴趣分析，请先多阅读并标记文章`.

- [ ] **Step 4: Register canonical and compatibility routes once**

Create `backend/cmd/server/interest_routes.go`:

```go
package main

import (
	"github.com/bytedance/rss-pal/internal/api"
	"github.com/gin-gonic/gin"
)

func registerInterestRoutes(routes gin.IRoutes, handler *api.InterestsHandler) {
	routes.GET("/interests/latest", handler.Latest)
	routes.POST("/interests/generate", handler.Generate)
	routes.GET("/insights/latest", handler.LatestLegacy)
	routes.POST("/insights/generate", handler.Generate)
}
```

In `backend/cmd/server/main.go`, construct `userInterestsRepo` and
`interestsHandler`, remove the inline insight route registrations, and call:

```go
registerInterestRoutes(apiGroup, interestsHandler)
```

- [ ] **Step 5: Format and run API tests GREEN**

Run:

```bash
cd backend
gofmt -w internal/api/interests.go internal/api/interests_test.go cmd/server/interest_routes.go cmd/server/interest_routes_test.go cmd/server/main.go
go test ./cmd/server ./internal/api -count=1
```

Expected: both packages PASS and the route-table test finds all four routes.

- [ ] **Step 6: Commit the API slice**

```bash
git add backend/internal/api backend/cmd/server
git commit -m "feat(api): expose canonical interest endpoints"
```

---

### Task 4: Interest worker terminology and environment compatibility

**Files:**
- Create: `backend/cmd/worker/interests.go`
- Delete: `backend/cmd/worker/insights.go`
- Create: `backend/cmd/worker/interests_test.go`
- Delete: `backend/cmd/worker/insights_test.go`
- Modify: `backend/cmd/worker/main.go`
- Modify: `backend/cmd/worker/briefing.go`

- [ ] **Step 1: Add failing environment compatibility tests**

Carry `TestNextDaily0400CST` into `interests_test.go`, then add:

```go
func TestInterestRunNowEnabled(t *testing.T) {
	t.Setenv("INTERESTS_RUN_NOW", "1")
	t.Setenv("INSIGHTS_RUN_NOW", "")
	if !interestRunNowEnabled() {
		t.Fatal("canonical INTERESTS_RUN_NOW should enable the job")
	}
}

func TestInterestRunNowEnabledAcceptsLegacyAlias(t *testing.T) {
	t.Setenv("INTERESTS_RUN_NOW", "")
	t.Setenv("INSIGHTS_RUN_NOW", "1")
	if !interestRunNowEnabled() {
		t.Fatal("legacy INSIGHTS_RUN_NOW should remain compatible")
	}
}
```

- [ ] **Step 2: Run worker tests and verify RED**

Run:

```bash
cd backend
go test ./cmd/worker -run 'TestInterestRunNowEnabled|TestNextDaily0400CST' -count=1
```

Expected: build FAIL because `interestRunNowEnabled` does not exist.

- [ ] **Step 3: Rename the scheduler and implement the env alias**

Apply this mapping:

```text
insights.go                  -> interests.go
insights_test.go             -> interests_test.go
scheduleDailyInsightCron     -> scheduleDailyInterestCron
runDailyInsightJob           -> runDailyInterestJob
insightCronDeps              -> interestCronDeps
generateDailyInsights        -> generateDailyInterests
userInsightsRepo             -> userInterestsRepo
daily insight cron           -> daily interest cron
```

Add the compatibility helper and use it at scheduler startup:

```go
func interestRunNowEnabled() bool {
	return os.Getenv("INTERESTS_RUN_NOW") == "1" || os.Getenv("INSIGHTS_RUN_NOW") == "1"
}
```

The startup log should name `INTERESTS_RUN_NOW`; the source comment must state
that `INSIGHTS_RUN_NOW` is a temporary compatibility alias. Update
`backend/cmd/worker/main.go` wiring and the comparison comment in
`briefing.go`. Do not change the 04:00 schedule, decay factor, candidate caps,
AI calls, or pacing.

- [ ] **Step 4: Format and run worker tests GREEN**

Run:

```bash
cd backend
gofmt -w cmd/worker/interests.go cmd/worker/interests_test.go cmd/worker/main.go cmd/worker/briefing.go
go test ./cmd/worker -count=1
```

Expected: worker package PASS.

- [ ] **Step 5: Commit the worker slice**

```bash
git add backend/cmd/worker
git commit -m "refactor(worker): rename insight job to interest"
```

---

### Task 5: Terminology allowlist and full verification

**Files:**
- Modify only files found by the active-source scan whose old terminology is
  not one of the compatibility/persistence exceptions below.

- [ ] **Step 1: Scan active source for remaining insight terminology**

Run:

```bash
rg -n -i "insight" frontend/src backend \
  --glob '!backend/migrations/**' \
  --glob '!**/testdata/**'
```

Expected allowed matches only:

```text
frontend/src/App.tsx: legacy /insights redirect
backend/cmd/server/interest_routes.go: legacy /insights API aliases
backend/internal/api/interests.go: legacy response key "insight"
backend/cmd/worker/interests.go: legacy INSIGHTS_RUN_NOW alias
backend/internal/repository/interest.go: user_insights persistence table
backend/internal/repository/rls_migration_test.go: user_insights schema assertion
```

Remove or rename every other match. Do not edit historical migration files.

- [ ] **Step 2: Run complete frontend verification**

Run:

```bash
cd frontend
npm run check
npm run build
```

Expected: 37+ Vitest files pass, all legacy tests pass, and TypeScript/Vite
production build exits 0.

- [ ] **Step 3: Run complete backend verification**

Run:

```bash
cd backend
go test ./... -count=1
```

Expected: every backend package passes.

- [ ] **Step 4: Inspect the final diff and recommendation preservation**

Run:

```bash
git diff --check
git status --short
git diff --stat master...HEAD
test -f frontend/src/pages/RecommendedPage.tsx
rg -n "GetLinkSetRecommendations|/articles/recommended" backend frontend/src/api/client.ts
```

Expected: no whitespace errors, only planned files changed, `RecommendedPage`
still exists, and recommendation backend/client implementations remain present.

- [ ] **Step 5: Commit final cleanup if the scan required changes**

```bash
git add frontend/src backend
git commit -m "chore: finish interest terminology cleanup"
```

Skip this commit only when `git status --short` confirms there are no cleanup
changes after the four implementation commits.
