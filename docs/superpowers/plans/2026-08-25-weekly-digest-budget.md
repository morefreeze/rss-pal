# Weekly Digest Budget Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise the weekly digest output budget to 4096 tokens and make worker startup generate any missing digests for the two most recently completed weeks.

**Architecture:** Keep weekly generation on the existing summarizer and repository paths. Replace the weekly call's literal token budget with a named constant, and add a pure Shanghai-week helper that feeds two idempotent catch-up calls into the existing `UserIDsMissing` boundary.

**Tech Stack:** Go, `net/http/httptest`, existing RSS Pal worker and repository packages.

---

### Task 1: Raise the weekly digest output budget

**Files:**
- Create: `backend/internal/ai/weekly_digest_test.go`
- Modify: `backend/internal/ai/weekly_digest.go:9-33`

- [ ] **Step 1: Write the failing request-budget test**

Create `backend/internal/ai/weekly_digest_test.go`:

```go
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateWeeklyIntroUsesWeeklyTokenBudget(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"本周主题导语"}}]}`)
	}))
	defer srv.Close()

	s := NewSummarizerWithModel("key", srv.URL, "model")
	_, err := s.GenerateWeeklyIntro(context.Background(), []WeeklyDigestItem{{
		Title:        "文章",
		SummaryBrief: "摘要",
	}})
	if err != nil {
		t.Fatalf("GenerateWeeklyIntro: %v", err)
	}
	if got.MaxTokens != 4096 {
		t.Fatalf("max_tokens = %d, want 4096", got.MaxTokens)
	}
}
```

- [ ] **Step 2: Run the focused test and verify the expected failure**

Run:

```bash
cd backend
go test ./internal/ai -run TestGenerateWeeklyIntroUsesWeeklyTokenBudget -count=1
```

Expected: FAIL with `max_tokens = 600, want 4096`.

- [ ] **Step 3: Implement the named weekly token budget**

In `backend/internal/ai/weekly_digest.go`, add the constant below the imports and use it in the existing call:

```go
const weeklyDigestMaxTokens = 4096
```

```go
return s.call(ctx, prompt, weeklyDigestMaxTokens)
```

- [ ] **Step 4: Run the focused AI tests**

Run:

```bash
cd backend
go test ./internal/ai -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the budget change**

```bash
git add backend/internal/ai/weekly_digest.go backend/internal/ai/weekly_digest_test.go
git commit -m "fix(ai): raise weekly digest token budget"
```

### Task 2: Catch up the two most recently completed weeks

**Files:**
- Modify: `backend/cmd/worker/briefing.go:15-31,274-283`
- Modify: `backend/cmd/worker/briefing_test.go:40-49`

- [ ] **Step 1: Write the failing completed-week helper test**

Append to `backend/cmd/worker/briefing_test.go`:

```go
func TestRecentCompletedWeekStarts(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
	}{
		{name: "midweek", now: sh(2026, time.August, 25, 10, 0)},
		{name: "monday boundary", now: sh(2026, time.August, 24, 0, 1)},
	}
	want := []time.Time{
		sh(2026, time.August, 17, 0, 0),
		sh(2026, time.August, 10, 0, 0),
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recentCompletedWeekStarts(tt.now, 2)
			if len(got) != len(want) {
				t.Fatalf("len = %d, want %d", len(got), len(want))
			}
			for i := range want {
				if !got[i].Equal(want[i]) {
					t.Errorf("week[%d] = %s, want %s", i, got[i], want[i])
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run the focused worker test and verify the expected failure**

Run:

```bash
cd backend
go test ./cmd/worker -run TestRecentCompletedWeekStarts -count=1
```

Expected: build failure with `undefined: recentCompletedWeekStarts`.

- [ ] **Step 3: Implement the helper and two-week catch-up loop**

In `backend/cmd/worker/briefing.go`, add the catch-up count beside the existing briefing hour:

```go
const (
	briefingHourCST       = 5
	weeklyCatchUpWeekCount = 2
)
```

Add this helper below `isMondayShanghai`:

```go
func recentCompletedWeekStarts(now time.Time, count int) []time.Time {
	thisWeek := api.MondayLabel(now)
	weeks := make([]time.Time, 0, count)
	for k := 1; k <= count; k++ {
		weeks = append(weeks, thisWeek.AddDate(0, 0, -7*k))
	}
	return weeks
}
```

Replace the final weekly call in `runBriefingCatchUp` with:

```go
for _, weekStart := range recentCompletedWeekStarts(now, weeklyCatchUpWeekCount) {
	fireWeeklyForAllUsers(ctx, deps, weekStart)
}
```

Update the function comment to say it generates the last two completed weeklies.

- [ ] **Step 4: Format and run focused worker tests**

Run:

```bash
gofmt -w backend/cmd/worker/briefing.go backend/cmd/worker/briefing_test.go
cd backend
go test ./cmd/worker -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the catch-up change**

```bash
git add backend/cmd/worker/briefing.go backend/cmd/worker/briefing_test.go
git commit -m "fix(worker): catch up two weekly digests"
```

### Task 3: Verify the complete backend change

**Files:**
- Verify only; no additional production files.

- [ ] **Step 1: Run focused regression tests together**

```bash
cd backend
go test ./internal/ai ./cmd/worker -count=1
```

Expected: both packages PASS.

- [ ] **Step 2: Run the full backend suite**

```bash
cd backend
go test ./...
```

Expected: exit code 0 with no failing packages.

- [ ] **Step 3: Inspect the final diff and worktree state**

```bash
git diff master...HEAD --check
git status --short --branch
```

Expected: no whitespace errors and a clean worktree on `codex/fix-weekly-digest-budget`.

- [ ] **Step 4: Stop at the production deployment boundary**

Report the verified commits and request explicit confirmation before pushing or rebuilding/recreating the Tencent worker. Deployment verification must cover the production revision, worker image, logs for both target week labels, database rows, and authenticated weekly API responses.
