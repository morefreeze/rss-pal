# Extension: Return Article Link After Capture

## Problem

When the Chrome extension successfully captures a page into 网摘, the popup shows a generic success status (e.g. "✅ 已加入网摘: <title>") but offers no way to jump to the saved article. The user has no way to verify the result or continue reading without manually opening the RSS Pal site and finding the article in the list.

The server already returns `article_id` in every successful response — the data is on the wire, just unused on the client.

## Scope

Surface the captured article's URL as a clickable link in the popup status area. Out of scope: auto-opening tabs, settings for link behavior, handling the edge case where the user isn't logged in to the frontend.

## Behavior

| Server response       | Status UI                                                       |
|-----------------------|-----------------------------------------------------------------|
| `created` (HTTP 201)  | `✅ 已加入网摘: <title> · 打开文章 ↗`                            |
| `updated` (HTTP 200)  | `✅ 已更新文章: <title> · 打开文章 ↗`                            |
| Overwrite success     | `✅ 覆盖成功 · 打开文章 ↗`                                       |
| `duplicate` prompt    | Existing duplicate prompt block, plus `· 打开文章 ↗` at the end |
| Errors                | Unchanged                                                        |

- `打开文章 ↗` is an `<a>` with `href="${serverUrl}/articles/${article_id}"`, `target="_blank"`, `rel="noopener noreferrer"`.
- When `article_id` is missing or falsy in the response, render the status without the link (defensive — server is expected to always include it).
- `serverUrl` is trimmed of trailing slashes (same `serverUrl.replace(/\/+$/, '')` as the existing `sendToServer`).

## Implementation Notes

Files touched:

- **`extension/popup.js`**
  - Existing `showStatus(type, msg)` already passes `msg` through `innerHTML`, so the link can be appended as an HTML string.
  - Add a small helper `buildArticleLink(serverUrl, articleId) -> string` that returns either `' · <a class="article-link" href="..." target="_blank" rel="noopener noreferrer">打开文章 ↗</a>'` or `''`.
  - In `doCapture`, after `sendToServer` returns, pass `result.article_id` into the success and duplicate branches.
  - In the `overwriteBtn` click handler, do the same with the overwrite response's `article_id`.
  - The `duplicatePrompt` block currently builds `dupMessage.textContent = ...`. Switch to `dupMessage.innerHTML = escapeHtml(message) + buildArticleLink(...)` — and add a tiny `escapeHtml` helper since `message` originates from server JSON (low trust). Apply the same escape to any string interpolated into the success status (titles can contain `<`).

- **`extension/style.css`**
  - Add `.article-link { color: inherit; text-decoration: underline; margin-left: 4px; }` (or whatever matches the existing status palette — pick by reading the file).

- **`extension/manifest.json`**
  - Bump `version` from `1.2.1` to `1.2.2` (per the project's "bump on every change" rule).

- **`extension/popup.html`** — no change.

## Security

Status messages and titles now go through `innerHTML` (they already did, but only with server-built strings). Add an `escapeHtml` helper and escape any user/server-supplied text (`title`, `message`) before concatenating with the link HTML. `article_id` is a number, so it interpolates safely.

## Testing

Manual verification in Chrome after `docker-compose up -d --build frontend` is not required (extension is loaded directly from disk), but the user must reload the unpacked extension in `chrome://extensions/` and confirm the bumped version (1.2.2) appears.

Smoke flow:

1. Capture a new page → status shows `· 打开文章 ↗`; clicking opens the article in a new tab.
2. Capture the same page again with significantly worse content → duplicate prompt shows; link is present and points at the existing article.
3. Click "覆盖" → success status shows link.
4. Capture a page with a title containing `<script>` → status renders the literal text, not an executed script.
