# Persistent list filters

Follow-up requested by the user: also save unread/saved checkbox choices and classification. Persist unreadOnly, savedOnly, articlesGrouped (topic classification view), and articleTagFilter (all/untagged/tag) in localStorage with session fallback, matching the approved sort preference design.

Save both checkbox states together so the existing mutual-exclusion behavior survives launches. Validate tag IDs and kinds. A tag filter takes precedence over an old conflicting grouped setting. Keep current subscription and clip routing behavior.

Implementation and verification: extend the existing component regression suite; prove fresh-session behavior fails first, implement centralized persistence, run frontend check/build, commit and deploy via Tencent workflow; verify remote revision, runtime and public asset bytes.
