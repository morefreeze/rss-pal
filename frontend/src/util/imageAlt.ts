// Mirror of backend/internal/rss/content.go::flattenImageAltBlankLines.
// Collapses blank lines inside markdown image alt text so the image syntax
// stays on a single logical line. Without this, CommonMark treats the blank
// line as a paragraph break and the image renders as literal text instead of
// an <img>. Kept here for client-side cleanup of articles already stored in
// the DB before the server-side strip was added.

const IMAGE_ALT_WITH_BLANK_LINE_RE = /!\[([^\]]*\n[ \t]*\n[^\]]*)\]\(([^)\s]+)\)/g
const BLANK_LINE_RUN_RE = /[ \t]*\n([ \t]*\n)+[ \t]*/g

export function flattenImageAltBlankLines(md: string): string {
  return md.replace(IMAGE_ALT_WITH_BLANK_LINE_RE, (_match, alt: string, url: string) => {
    const flattened = alt.replace(BLANK_LINE_RUN_RE, ' ').trim()
    return `![${flattened}](${url})`
  })
}
