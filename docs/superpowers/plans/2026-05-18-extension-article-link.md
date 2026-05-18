# Extension Article Link Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After the Chrome extension captures a page into 网摘, show a clickable "打开文章 ↗" link in the popup status area pointing at `${serverUrl}/articles/${article_id}`.

**Architecture:** Frontend-only change inside `extension/`. The server already returns `article_id` in every capture response; the popup just needs to render it as an anchor in the existing status / duplicate-prompt elements. Add a tiny `escapeHtml` helper because the status text already flows through `innerHTML` and now mixes server-supplied titles with HTML — we need to neutralize the XSS surface that mix introduces.

**Tech Stack:** Vanilla JS (Chrome extension MV3 popup), plain CSS.

**Spec:** `docs/superpowers/specs/2026-05-18-extension-article-link-design.md`

---

## File Map

- **Modify** `extension/popup.js` — add `escapeHtml` + `buildArticleLink` helpers; thread `result.article_id` into success / duplicate / overwrite branches; escape server-supplied text before composing the status HTML.
- **Modify** `extension/style.css` — add `.article-link` rule that fits the existing status palette.
- **Modify** `extension/manifest.json` — bump `version` from `1.2.1` to `1.2.2`.

No new files. No backend changes. No tests (the extension has no test harness; verification is manual in Chrome).

---

### Task 1: Implement the link rendering

**Files:**
- Modify: `extension/popup.js` (lines ~28–50 helpers, line ~254 dup branch, line ~257 success branch, line ~279 overwrite branch)
- Modify: `extension/style.css` (after the `.status-warning` block ending around line 165)
- Modify: `extension/manifest.json` (line 4)

- [ ] **Step 1: Add `escapeHtml` and `buildArticleLink` helpers in popup.js**

In `extension/popup.js`, immediately after the `hideDuplicate` function (right before `function setLoading` around line 44), insert:

```javascript
  function escapeHtml(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function buildArticleLink(serverUrl, articleId) {
    if (!articleId) return '';
    const base = String(serverUrl || '').replace(/\/+$/, '');
    if (!base) return '';
    const href = base + '/articles/' + encodeURIComponent(articleId);
    return ' · <a class="article-link" href="' + href +
      '" target="_blank" rel="noopener noreferrer">打开文章 ↗</a>';
  }
```

- [ ] **Step 2: Thread article_id into the success branch of `doCapture`**

In `extension/popup.js`, find the success path inside `doCapture` (around line 256–258):

```javascript
      } else {
        showStatus('success', '✅ ' + (result.message || '发送成功'));
      }
```

Replace with:

```javascript
      } else {
        const link = buildArticleLink(serverUrl, result.article_id);
        showStatus('success', '✅ ' + escapeHtml(result.message || '发送成功') + link);
      }
```

- [ ] **Step 3: Thread article_id into the duplicate prompt**

In `extension/popup.js`, find the duplicate branch inside `doCapture` (around lines 252–255):

```javascript
      if (result.status === 'duplicate' && !force) {
        hideStatus();
        dupMessage.textContent = result.message || '该文章已存在，是否覆盖？';
        duplicatePrompt.style.display = 'block';
      }
```

Replace with:

```javascript
      if (result.status === 'duplicate' && !force) {
        hideStatus();
        const link = buildArticleLink(serverUrl, result.article_id);
        dupMessage.innerHTML = escapeHtml(result.message || '该文章已存在，是否覆盖？') + link;
        duplicatePrompt.style.display = 'block';
      }
```

- [ ] **Step 4: Thread article_id into the overwrite success branch**

In `extension/popup.js`, find the `overwriteBtn` click handler (around lines 270–285). Locate the `try` block that calls `sendToServer` and shows status. Replace:

```javascript
    try {
      const result = await sendToServer(
        lastCapture.url, lastCapture.title, lastCapture.html,
        lastCapture.serverUrl, lastCapture.token, true
      );
      showStatus('success', '✅ ' + (result.message || '覆盖成功'));
    } catch (err) {
```

with:

```javascript
    try {
      const result = await sendToServer(
        lastCapture.url, lastCapture.title, lastCapture.html,
        lastCapture.serverUrl, lastCapture.token, true
      );
      const link = buildArticleLink(lastCapture.serverUrl, result.article_id);
      showStatus('success', '✅ ' + escapeHtml(result.message || '覆盖成功') + link);
    } catch (err) {
```

- [ ] **Step 5: Escape the error path too (defensive — server errors flow through innerHTML)**

In `extension/popup.js`, both `doCapture` and the `overwriteBtn` handler call:

```javascript
      showStatus('error', '❌ ' + err.message);
```

Replace **both** occurrences with:

```javascript
      showStatus('error', '❌ ' + escapeHtml(err.message));
```

Use `replace_all` if convenient, or apply the edit twice with surrounding context.

- [ ] **Step 6: Add `.article-link` style in style.css**

In `extension/style.css`, immediately after the `.status-warning` block (ending around line 165), insert:

```css
.article-link {
  color: inherit;
  text-decoration: underline;
  font-weight: 500;
  margin-left: 2px;
}

.article-link:hover {
  opacity: 0.8;
}
```

`color: inherit` keeps the link in the green/red/amber of whichever status container it lands in.

- [ ] **Step 7: Bump manifest version**

In `extension/manifest.json`, change:

```json
  "version": "1.2.1",
```

to:

```json
  "version": "1.2.2",
```

- [ ] **Step 8: Manual smoke test**

The extension has no automated test harness. Reload the unpacked extension in `chrome://extensions/` (developer mode → reload arrow on the RSS Pal card) and confirm in the card header that version reads **1.2.2**.

Then walk through these four cases on any signed-in dev environment:

1. **New capture:** navigate to a fresh article URL, open the popup, click "⭐ 发送到 RSS Pal". Status should read `✅ 已加入网摘: <title> · 打开文章 ↗`. Clicking the link opens the article in a new tab.
2. **Duplicate prompt:** capture the same page again immediately (worker hasn't backfilled summaries, so content len/images compare equal — should fall through to "updated" not "duplicate"). To force the prompt, capture a page whose `js_content` was much richer the first time and is now blocked by a paywall, or temporarily lower `duplicateOverwriteRatio` in backend to provoke it. The duplicate prompt should show the comparison message plus `· 打开文章 ↗` as the last item before the 覆盖/取消 buttons.
3. **Overwrite:** click 覆盖 in the duplicate prompt. Status should read `✅ 已更新文章: <title> · 打开文章 ↗`.
4. **XSS guard:** if you have shell access to the DB, `UPDATE articles SET title='<script>alert(1)</script>' WHERE id=<id>` for a test article, then re-capture its URL. The status should render the literal `<script>...</script>` text, not execute it. Revert the title afterwards.

Report which cases you verified and any that you skipped (e.g. if you can't easily provoke the duplicate path, say so — don't claim success without evidence).

- [ ] **Step 9: Commit**

```bash
git add extension/popup.js extension/style.css extension/manifest.json
git commit -m "$(cat <<'EOF'
feat(extension): show 打开文章 link after capture

Returns the article URL inline in the popup status (and inside the
duplicate prompt) so users can jump straight to the saved article
instead of navigating manually. Escapes server-supplied text since the
status now mixes user content with link HTML.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```
