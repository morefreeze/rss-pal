# BestBlogs full newsletter repair

Goal: retain the existing valid Chinese newsletter feed URL, extract the introduction and all recommendation articles with resolved links, and repair only the latest affected issue (3251).

1. Reproduce generic selector failure using the existing reduced multi-article fixture.
2. Apply host/path-scoped BestBlogs content selection; verify full HTTP extraction including relative links and all recommendations.
3. Run RSS and caller checks, review the diff, integrate into origin/master and deploy via Deploy Tencent.
4. Back up article 3251, fetch with the deployed parser, compare every original recommendation heading and description, update guarded by original content hash, and verify database/API persistence and service health.

No feed URL change is needed: the configured /zh/feeds/rss/newsletter endpoint is valid. Preserve unrelated workspace edits and historical articles.
