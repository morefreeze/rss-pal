# HN single-post capture

Confirmed scope: only explicit HN item URLs, through subscription input, bookmarklet and extension clip. No discovery or scheduled polling.

1. Add an HN public-API reader before generic content extraction. Canonicalize item URLs, reject invalid IDs, non-story and unavailable items. Preserve story title/body/source, author, score, comment count; ranked discussion branches with author, date and permalink. Bound to 20 root branches, 10 replies per level, 3 levels, 120 fetched comments and 1 MiB per response. Mark omitted replies. Fail explicitly on API errors.
2. Route bookmarklet capture (also extension HTML clip) to the same reader, preserving authentication, duplicate handling and database writes. HN content comes from API rather than generic captured page chrome.
3. Recognize HN item URLs in subscription form and save via existing one-off clip endpoint with expand=false. Use metadata title and canonical URL; re-fetch uses the same reader.
4. Test URL boundaries, story/comment preservation, truncation/errors, bookmarklet persistence, form behavior and generic regressions. Review changes; push actual master and verify established Tencent deployment plus HN capture round trip.
