# Search contexts and in-article navigation

Confirmed: result descriptions show context around matches, matching text is red with accessible light/dark theme variants. Opening a result activates search across title, AI brief/detailed summary and article content. A fixed bottom toolbar shows current/total and previous/next controls that wrap. Exiting removes highlights. Existing reader modes, links, code and media remain functional.

Return a bounded plain-text search snippet from the search API without shipping full content. Highlight using React/HAST text nodes, never untrusted HTML or destructive DOM text replacement. The same matcher handles literal case-insensitive queries and existing pinyin fallback. Inline formatting may split one logical hit into several marks, counted as one. Do not match across paragraphs or hidden math annotation copies.

Carry q in list/detail URLs, retain result navigation and return context, cancel stale result requests. Detail scroll restoration yields to search positioning. Reading mode expands summaries during search so summary matches remain reachable. URL/source-only results show their matching context and honestly report zero in-article matches.

Verify snippet bounds/Unicode/Markdown, text safety, inline matches, counts/wrap/close, stale requests, both reading layouts and five theme contrasts. Preserve unrelated main-worktree edits. Integrate latest master and deploy through established Tencent workflow; verify exact revision, runtime health and public assets.
