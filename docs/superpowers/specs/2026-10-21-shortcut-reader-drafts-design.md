# Shortcut links belong to the source article's pending list

The user clarified that “this article's capture queue” is the original RSS Pal article's pending link list, which waits for an explicit Start Fetch action. The previous implementation incorrectly created independent clip articles for an automatic worker. This is a correction of that behavior, not a second queue UI.

The existing pending list is localStorage-only. Add a private durable `article_capture_drafts` table for links submitted by the shortcut; merge these entries into the original page's existing local draft list. Preserve manual selection and its local/offline behavior. An authenticated GET reads server drafts and already fetched child URLs; DELETE removes selected draft URLs. Batch-fetch deletes only the submitted server drafts in the same transaction that creates child jobs. The page syncs on focus, visibility/pageshow and bounded polling while visible; route changes and in-flight deletion/submission cannot resurrect old entries.

The paired shortcut still sends a URL. Resolve its source only among the credential owner's visible articles containing that exact link (with URL boundaries, not a prefix match), or validate an explicitly supplied source_article_id. A unique source accepts the draft; no match or multiple matches returns a clear error without creating a clip or fetching any page. Keep parent_article_id in the response for installed shortcut compatibility, now referring to the source article. Disable the erroneous automatic shortcut worker. Do not change ordinary bookmarklet captures or confirmed batch fetching.

API contract:

- POST /api/shortcuts/capture remains capture-credential-only, validates public HTTP(S) input without network I/O, inserts one deduplicated draft for the resolved source, returns 202 {parent_article_id, status:"drafted", message}. Already fetched links return 200 {parent_article_id,status:"already_fetched",message} without adding a draft.
- GET /api/articles/:id/capture-drafts is login/RLS protected; returns {drafts:[{url,title,addedAt}],fetched_link_urls:[...]}. Empty lists are arrays. Cache-Control no-store.
- DELETE /api/articles/:id/capture-drafts accepts {urls:[...]}; requires ownership and deletes only these URLs for that user/article. Idempotent success 204.

Store no user token or private draft data in public shares. Migration 059 is added to the established startup migration chain, including explicit application-role grants. Deployment must verify the migrated table/policy/grants, deployed SHA and exact reader surface; retain unrelated checkout work and check unrelated service IDs before/after.

The source-confirmed user incident is target placeholder 3285, owner 1, from source article 3283. After release, export a before snapshot, add its target to the source's draft list, then hide the erroneous empty standalone placeholder reversibly and mark it failed/moved only with exact owner/URL/content/state guards. Do not delete or rewrite an already saved article. Verify the original article's pending list contains the link and no child is created until explicit confirmation.
