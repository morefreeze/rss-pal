# Bookmarklet Full-Content Capture Implementation Plan

> **For agentic workers:** Choose the execution mode with the Execution Routing section below. Use superpowers:executing-plans for small or tightly coupled plans, and superpowers:subagent-driven-development for larger plans with independently reviewable tasks. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Save the complete Markdown extracted by the HTML bookmarklet/extension path, including UTF-8 content beyond the former 50,000-byte boundary.

**Architecture:** Keep the existing shared capture endpoint, 4 MiB request limit, Markdown extraction, duplicate handling, and repository insertion. Remove only the post-extraction 50,000-byte slice; downstream summarization retains its independent 8,000-rune prompt cap.

**Tech Stack:** Go 1.25, Gin, goquery, PostgreSQL repository layer, Go testing package

---

## File map

- Modify `backend/internal/api/bookmarklet_test.go`: add a regression test that crosses the old byte boundary with Chinese text and proves the tail survives.
- Modify `backend/internal/api/bookmarklet.go`: return the complete extracted Markdown without storage truncation.

### Task 1: Add the long UTF-8 article regression test

**Files:**
- Modify: `backend/internal/api/bookmarklet_test.go`
- Test: `backend/internal/api/bookmarklet_test.go`

- [ ] **Step 1: Write the failing test**

Add `unicode/utf8` to the import block and add this test after the existing extraction tests:

```go
func TestExtractContentFromHTMLPreservesFullUTF8ContentAboveLegacyLimit(t *testing.T) {
	const tailMarker = "CHAPTER-END-完整"
	body := strings.Repeat("中", 16667) + tailMarker
	html := `<html><body><article><p>` + body + `</p></article></body></html>`

	got, err := extractContentFromHTML(html, "https://example.com/book/chapter1/")
	if err != nil {
		t.Fatalf("extractContentFromHTML: %v", err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("extracted content is not valid UTF-8 near the legacy 50,000-byte boundary")
	}
	if len(got) <= 50000 {
		t.Fatalf("extracted content length = %d, want > 50000", len(got))
	}
	if !strings.Contains(got, tailMarker) {
		t.Fatalf("extracted content lost tail marker %q", tailMarker)
	}
	if strings.HasSuffix(got, "...") {
		t.Fatalf("extracted content still has the legacy truncation suffix")
	}
}
```

- [ ] **Step 2: Format the test file**

Run from `backend/`:

```bash
gofmt -w internal/api/bookmarklet_test.go
```

Expected: the new import and test use standard Go formatting.

- [ ] **Step 3: Run the focused test and verify RED**

Run from `backend/`:

```bash
go test ./internal/api -run TestExtractContentFromHTMLPreservesFullUTF8ContentAboveLegacyLimit -count=1 -v
```

Expected: FAIL with `extracted content is not valid UTF-8 near the legacy 50,000-byte boundary`, proving the test reproduces the production failure.

### Task 2: Remove HTML capture storage truncation

**Files:**
- Modify: `backend/internal/api/bookmarklet.go:486-488`
- Test: `backend/internal/api/bookmarklet_test.go`

- [ ] **Step 1: Delete the legacy truncation block**

Remove only these lines from `extractContentFromHTML`:

```go
	if len(content) > 50000 {
		content = content[:50000] + "..."
	}
```

Keep the existing final return:

```go
	return strings.TrimSpace(content), nil
```

- [ ] **Step 2: Run the focused test and verify GREEN**

Run from `backend/`:

```bash
go test ./internal/api -run TestExtractContentFromHTMLPreservesFullUTF8ContentAboveLegacyLimit -count=1 -v
```

Expected: PASS. The result is valid UTF-8, exceeds 50,000 bytes, contains `CHAPTER-END-完整`, and has no synthetic truncation suffix.

- [ ] **Step 3: Run the API package regression tests**

Run from `backend/`:

```bash
go test ./internal/api -count=1
```

Expected: PASS with no package failures.

- [ ] **Step 4: Commit the tested fix**

Run from the worktree root:

```bash
git add backend/internal/api/bookmarklet.go backend/internal/api/bookmarklet_test.go
git commit -m "fix(api): preserve full bookmarklet content"
```

Expected: one implementation commit containing only the handler and its regression test.

### Task 3: Verify the complete backend and branch state

**Files:**
- No additional file changes.

- [ ] **Step 1: Run the complete backend test suite**

Run from `backend/`:

```bash
go test ./... -count=1
```

Expected: every backend package passes.

- [ ] **Step 2: Run repository hygiene checks**

Run from the worktree root:

```bash
git diff --check HEAD^ HEAD
git status --short --branch
git log --oneline -3
```

Expected: no whitespace errors, a clean `codex/bookmarklet-full-content` worktree, and separate design, plan, and implementation commits at the branch tip.

- [ ] **Step 3: Report the delivery boundary**

Report the verified local fix and exact test commands. Do not push, merge, deploy, restart containers, or mutate Tencent production without a separate explicit request.

## Execution Routing

Use **Inline Execution** with `superpowers:executing-plans`. The change is one tightly coupled regression test and one deletion in the same backend package; subagent delegation would add coordination without an independent review boundary.
