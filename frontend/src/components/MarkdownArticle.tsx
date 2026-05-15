import { memo, useContext, useMemo, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import 'highlight.js/styles/github.css'
import 'katex/dist/katex.min.css'
import { stripMathShadow, escapeAmbiguousMathDollars } from '../util/mathShadow'
import { flattenImageAltBlankLines } from '../util/imageAlt'
import VideoEmbed from './VideoEmbed'
import { parsePlaceholder } from './parseVideoPlaceholder'
import { CodeWrapContext } from './CodeWrapContext'

type Props = {
  source: string
}

const AVATAR_ATTR_KEYWORDS = [
  'avatar', 'gravatar', 'profile', 'author',
  'user-pic', 'userpic', 'headshot',
]
const AVATAR_URL_KEYWORDS = [
  'gravatar.com', '/avatar/', '/avatars/',
]

// isAvatarImg mirrors the server-side detector (Signal 1 only — class/id/width
// /height attributes don't survive markdown round-trip, so dimension matching
// is unreachable client-side). Returns true if the image's URL or alt text
// contains any avatar keyword.
function isAvatarImg(src: string | undefined, alt: string | undefined): boolean {
  const url = (src ?? '').toLowerCase()
  for (const kw of AVATAR_URL_KEYWORDS) {
    if (url.includes(kw)) return true
  }
  const altLower = (alt ?? '').toLowerCase()
  if (!altLower) return false
  for (const kw of AVATAR_ATTR_KEYWORDS) {
    if (altLower.includes(kw)) return true
  }
  return false
}

// Returns the plain-text content of paragraph children when it consists
// of a single text run, otherwise null. Used to detect placeholder
// paragraphs without false-positives on rich content.
function extractParagraphText(children: unknown): string | null {
  if (typeof children === 'string') return children
  if (Array.isArray(children)) {
    if (children.length !== 1) return null
    return extractParagraphText(children[0])
  }
  return null
}

// Wraps each fenced <pre> so the wrap state can be toggled per-block on
// top of the global CodeWrapContext setting. Local override is null
// (= follow global) until the user clicks the toggle; reload resets it.
function CodeBlock({ children, ...rest }: React.HTMLAttributes<HTMLPreElement>) {
  const globalWrap = useContext(CodeWrapContext)
  const [override, setOverride] = useState<boolean | null>(null)
  const wrapped = override ?? globalWrap
  return (
    <div className="code-block-wrap" data-wrap={wrapped ? 'true' : 'false'}>
      <button
        type="button"
        className="code-wrap-toggle"
        aria-label={wrapped ? '关闭自动换行' : '开启自动换行'}
        title={wrapped ? '关闭自动换行' : '开启自动换行'}
        onClick={() => setOverride(!wrapped)}
      >
        {wrapped ? '↵' : '→'}
      </button>
      <pre {...rest}>{children}</pre>
    </div>
  )
}

// Module-scoped plugin lists and component overrides. Hoisted out of the
// render function so their references are stable across re-renders —
// otherwise ReactMarkdown sees a fresh `components` object each render,
// rebuilds the entire AST + React tree, and lazy <img> elements get
// remounted (cancelling and re-issuing image fetches mid-load).
const REMARK_PLUGINS = [remarkGfm, remarkMath]
const REHYPE_PLUGINS = [rehypeHighlight, rehypeKatex]
const COMPONENTS: Components = {
  img: ({ src, alt, ...rest }) => {
    if (isAvatarImg(src, alt)) return null
    const proxied = src
      ? `/api/proxy/image?url=${encodeURIComponent(src)}`
      : undefined
    return (
      <img
        src={proxied}
        alt={alt ?? ''}
        loading="lazy"
        decoding="async"
        style={{ maxWidth: '100%', height: 'auto' }}
        {...rest}
      />
    )
  },
  a: ({ href, children, ...rest }) => (
    <a href={href} target="_blank" rel="noopener noreferrer" {...rest}>
      {children}
    </a>
  ),
  p: ({ children, ...rest }) => {
    const text = extractParagraphText(children)
    if (text) {
      const v = parsePlaceholder(text)
      if (v) return <VideoEmbed {...v} />
    }
    return <p {...rest}>{children}</p>
  },
  pre: CodeBlock,
}

// Rewrites <img src="..."> to go through the backend proxy so hotlink-
// protected sites (WeChat, Zhihu) actually render. Author/profile avatars
// are dropped entirely (see isAvatarImg). LaTeX math via remark-math +
// rehype-katex; Jina Reader's shadow duplicate is removed via stripMathShadow
// before parsing. External links open in a new tab.
//
// Wrapped in React.memo so the parent (ArticlePage) re-rendering on every
// scroll-progress / activity-tick state change doesn't force a full
// markdown re-parse and image remount.
function MarkdownArticle({ source }: Props) {
  const cleaned = useMemo(
    () => flattenImageAltBlankLines(escapeAmbiguousMathDollars(stripMathShadow(source))),
    [source],
  )
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={REMARK_PLUGINS}
        rehypePlugins={REHYPE_PLUGINS}
        components={COMPONENTS}
      >
        {cleaned}
      </ReactMarkdown>
    </div>
  )
}

export default memo(MarkdownArticle)
