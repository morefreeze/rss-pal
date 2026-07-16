# Weibo Blogger Comment Links Design

## Context

Some Weibo resource posts do not put the actual download link in the post body. The author instead publishes the link in their first comment. Article 3784 is a concrete example: the RSS item body only says that Quark and Baidu resources are available, while the first comment by the blogger contains both URLs.

RSSHub already supports `displayComments=1` on the Weibo user route and returns the relevant comment for this post. RSS Pal currently loses that data for two reasons:

1. Weibo user URLs resolve to the RSSHub route without `displayComments=1`.
2. RSS item HTML is flattened with `StripHTML`, which removes link destinations. The worker then deep-fetches the Weibo status page and replaces the concise RSS body with a longer login/search page.

## Goals

- Include the first available top-level comment written by the Weibo post author.
- Exclude comments by other users and nested replies.
- Preserve direct resource links as clickable Markdown links.
- Keep the clean RSS body instead of deep-fetching Weibo status pages.
- Repair recent existing Weibo articles when a later feed fetch supplies a blogger comment.
- Keep feed ingestion available when no blogger comment is returned.

## Non-goals

- Displaying all Weibo comments or building a general comment reader.
- Calling Weibo APIs directly from RSS Pal.
- Detecting resource posts by keywords.
- Backfilling posts that are no longer present in the RSSHub feed window.

## Data flow

1. Resolve desktop and mobile Weibo profile URLs to `/weibo/user/:uid/displayComments=1`.
2. RSSHub fetches the posts and appends its `热门评论` block when comment data is available.
3. RSS Pal parses the RSS item description as HTML.
4. In the comment block, select the first top-level comment whose author link identifies the same `uid` as the subscribed profile.
5. Remove the original comment block, append only the selected comment under a `博主首评` heading, and discard nested replies.
6. Unwrap Weibo redirect links of the form `https://weibo.cn/sinaurl?u=<encoded target>` to the decoded target URL.
7. Convert the result to Markdown so anchors and images survive ingestion.
8. Skip full-page content fetching for Weibo status URLs.

If the description contains no comment block or no same-author comment, the normal post body is still converted and saved. Comment extraction failure must not make the feed fail.

## Existing articles

New articles use the enriched content during creation. When an item already exists, ingestion may update it only when all of the following are true:

- the item is a Weibo status;
- the newly parsed content contains a `博主首评` section;
- the stored content does not already contain that section, or differs from the enriched content.

When content changes, word-count metrics are recomputed and existing summaries are cleared so they can be regenerated from the corrected body. This path is idempotent.

## Availability and safety

- RSSHub remains responsible for Weibo cookies and comment API behavior.
- A post with no returned comments remains a valid article.
- Link unwrapping accepts only an absolute `http` or `https` target; malformed or non-web targets retain the original URL.
- UID matching uses parsed URL path segments rather than display names.
- The generic ingestion behavior for non-Weibo feeds remains unchanged.

## Verification

Tests will cover:

- profile URL resolution with `displayComments=1`;
- selecting only the first top-level same-author comment;
- dropping other users' comments and nested replies;
- unwrapping Quark and Baidu links;
- graceful handling when comments are absent;
- skipping deep fetch for Weibo status URLs;
- idempotent update of an existing enriched article and summary invalidation.

The local acceptance check will refresh the feed containing article 3784 and verify that its rendered body includes clickable direct Quark and Baidu links under `博主首评` without Weibo login/search boilerplate.
