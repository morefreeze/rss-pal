# English Article Justification Design

**Date:** 2026-08-15
**Status:** Approved for implementation

## Problem

English-heavy article bodies already receive `lang="en"` and enable automatic
hyphenation. This keeps most words intact and hyphenates long words near the
right edge, but the paragraphs still use the browser's default left alignment.
As a result, non-final lines end at visibly different positions.

## Goals

- Give English article paragraphs a more consistent right edge.
- Keep automatic English hyphenation and emergency wrapping for unbreakable
  content.
- Keep the final line of each paragraph naturally left aligned.
- Leave headings, code blocks, tables, and non-English article bodies unchanged.

## Options Considered

### Justify English prose only

Apply justification to paragraphs and list items inside a `.markdown-body`
whose language is English. The browser dynamically adjusts inter-word spacing
and uses the existing hyphenation rules when necessary. This is selected
because it produces book-like prose without changing unrelated Markdown
surfaces.

### Justify every Markdown surface

Applying justification to `.markdown-body` globally would also affect Chinese
articles, AI summaries, insights, and shared pages that do not participate in
article language detection. The scope is broader than the requested behavior.

### Force every line to the right edge

Justifying paragraph-final lines would create conspicuously large spaces in
short lines. Paragraph-final lines should remain aligned to the start edge.

## Design

Add a CSS rule scoped to English article bodies:

```css
.markdown-body[lang="en"] :is(p, li) {
  text-align: justify;
  text-align-last: start;
}
```

The existing language detection remains the source of truth. No JavaScript
layout measurement is needed: line width, font metrics, viewport changes, and
hyphenation are handled by the browser during layout. Existing
`hyphens: auto`, `overflow-wrap: break-word`, and `word-break: normal` rules
remain unchanged.

## Verification

- Extend the CSS contract test so English paragraphs and list items must use
  justification and keep their final line start-aligned.
- Run the focused frontend tests, the full frontend check, and the production
  build under Node 22.
- Render a representative English article at desktop and mobile widths and
  verify that non-final prose lines align to the right edge without stretching
  paragraph-final lines or affecting code blocks.
