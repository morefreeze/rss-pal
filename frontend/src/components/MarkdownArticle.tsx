import { createContext, memo, useContext, useEffect, useMemo, useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import type { Components, ExtraProps } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import remarkCjkFriendly from 'remark-cjk-friendly'
import remarkMath from 'remark-math'
import rehypeHighlight from 'rehype-highlight'
import rehypeKatex from 'rehype-katex'
import 'highlight.js/styles/github.css'
import 'katex/dist/katex.min.css'
import { stripMathShadow, escapeAmbiguousMathDollars } from '../util/mathShadow'
import { annotateArticleMarkdown, createArticleAnchorRemarkPlugin, findArticleAnchors, normalizeArticleAnchorSource } from '../util/articleAnchors'
import VideoEmbed from './VideoEmbed'
import { parsePlaceholder, type VideoEmbedData } from './parseVideoPlaceholder'
import { CodeWrapContext } from './CodeWrapContext'
import { ReaderActionContext } from '../reader/ReaderActionContext'
import { ReaderInteractionSurface } from '../reader/ReaderInteractionSurface'

type VideoIdentity = Pick<VideoEmbedData, 'platform' | 'id'>

type Props = {
  source: string
  // Optional map of original-URL → [width, height]. When present, the
  // matching <img> renders with explicit dimensions so the browser reserves
  // layout space before the bytes arrive — which prevents reading-progress
  // from regressing as lazy-loaded images decode mid-scroll.
  imageDimensions?: Record<string, [number, number]>
  // The primary stored video is already rendered above the article body.
  // Suppress only a matching inline placeholder to avoid duplicate players.
  suppressVideo?: VideoIdentity
}

// Carries the dimensions map down to the COMPONENTS.img override, which is
// defined at module scope (hoisted for ref stability — see comment below).
const ImageDimensionsContext = createContext<Record<string, [number, number]> | null>(null)
const SuppressedVideoContext = createContext<VideoIdentity | null>(null)

const AVATAR_ATTR_KEYWORDS = [
  'avatar', 'gravatar', 'profile', 'author',
  'user-pic', 'userpic', 'headshot',
]
const AVATAR_URL_KEYWORDS = [
  'gravatar.com', '/avatar/', '/avatars/',
]
const LATIN_LETTER_RE = /[A-Za-z]/g
const CJK_LETTER_RE = /[\u3040-\u30ff\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff\uac00-\ud7af]/g
const ESCAPED_VIDEO_PLACEHOLDER_RE = /\\\[\\\[video:(youtube|bilibili):([\w-]+)(?:\?([\w=&]+))?\\?\]\\?\]/g
const VIDEO_PLACEHOLDER_RE = /\[\[video:(youtube|bilibili):([\w-]+)(?:\?([\w=&]+))?]]/g

function detectArticleLang(source: string): string | undefined {
  const latinCount = source.match(LATIN_LETTER_RE)?.length ?? 0
  if (latinCount < 24) return undefined
  const cjkCount = source.match(CJK_LETTER_RE)?.length ?? 0
  return latinCount >= Math.max(24, cjkCount * 2) ? 'en' : undefined
}

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

function formatTimestamp(seconds: number): string {
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) {
    return `${h}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`
  }
  return `${m}:${s.toString().padStart(2, '0')}`
}

function buildInlineVideoLink(platform: VideoEmbedData['platform'], id: string, rawParams?: string): string {
  const params = new URLSearchParams(rawParams ?? '')
  const start = params.get('start')
  const page = params.get('page')
  const startSeconds = start && /^\d+$/.test(start) ? parseInt(start, 10) : 0
  const label = startSeconds > 0
    ? formatTimestamp(startSeconds)
    : platform === 'youtube'
      ? 'YouTube'
      : 'Bilibili'

  if (platform === 'youtube') {
    const url = `https://www.youtube.com/watch?v=${id}${startSeconds > 0 ? `&t=${startSeconds}` : ''}`
    return `[${label}](${url})`
  }

  const query = new URLSearchParams()
  if (page && /^\d+$/.test(page) && parseInt(page, 10) > 1) query.set('p', page)
  if (startSeconds > 0) query.set('t', String(startSeconds))
  const qs = query.toString()
  return `[${label}](https://www.bilibili.com/video/${id}${qs ? `?${qs}` : ''})`
}

export function normalizeVideoPlaceholders(source: string): string {
  return source
    .replace(
      ESCAPED_VIDEO_PLACEHOLDER_RE,
      (_match, platform: VideoEmbedData['platform'], id: string, params?: string) =>
        `[[video:${platform}:${id}${params ? `?${params}` : ''}]]`,
    )
    .split(/\r?\n/)
    .map((line) => {
      if (parsePlaceholder(line.trim())) return line
      return line.replace(
        VIDEO_PLACEHOLDER_RE,
        (_match, platform: VideoEmbedData['platform'], id: string, params?: string) =>
          buildInlineVideoLink(platform, id, params),
      )
    })
    .join('\n')
}

function prepareArticleMarkdown(source: string): string {
  return annotateArticleMarkdown(normalizeVideoPlaceholders(
    normalizeArticleAnchorSource(escapeAmbiguousMathDollars(stripMathShadow(source))),
  ))
}

export function findStandaloneVideoAnchorID(source: string, video: VideoIdentity): string | undefined {
  const cleaned = prepareArticleMarkdown(source)
  const placeholderLine = cleaned.split(/\r?\n/).findIndex((line) => {
    const parsed = parsePlaceholder(line.trim())
    return parsed?.platform === video.platform && parsed.id === video.id
  })
  if (placeholderLine < 0) return undefined
  return findArticleAnchors(cleaned).find(({ kind, line }) => (
    kind === 'paragraph' && line === placeholderLine + 1
  ))?.id
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

type ArticleLinkProps = React.AnchorHTMLAttributes<HTMLAnchorElement> & ExtraProps

function ArticleLink({ href, children, className, node: _node, ...rest }: ArticleLinkProps) {
  const actionContext = useContext(ReaderActionContext)
  const anchorRef = useRef<HTMLAnchorElement>(null)
  const normalized = href && actionContext ? actionContext.normalizeLink(href) : null
  const state = normalized && actionContext ? actionContext.getLinkState(normalized) : null

  useEffect(() => {
    if (!normalized || !actionContext?.onLinkDiscovered || !anchorRef.current) return
    const title = (anchorRef.current.textContent ?? '').trim().replace(/\s+/g, ' ') || normalized
    actionContext.onLinkDiscovered({ url: normalized, title })
  }, [actionContext, children, normalized])

  return (
    <a
      ref={anchorRef}
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className={[className, state === 'draft' ? 'reader-link-draft' : null].filter(Boolean).join(' ') || undefined}
      data-reader-link-state={state ?? undefined}
      {...rest}
    >
      {children}
    </a>
  )
}

// Module-scoped plugin lists and component overrides. Hoisted out of the
// render function so their references are stable across re-renders —
// otherwise ReactMarkdown sees a fresh `components` object each render,
// rebuilds the entire AST + React tree, and lazy <img> elements get
// remounted (cancelling and re-issuing image fetches mid-load).
const REMARK_PLUGINS = [remarkGfm, remarkCjkFriendly, remarkMath]
const REHYPE_PLUGINS = [rehypeHighlight, rehypeKatex]
const COMPONENTS: Components = {
  img: ({ src, alt, ...rest }) => {
    if (isAvatarImg(src, alt)) return null
    // Same-origin images served by our backend (PDF clip images at
    // /api/articles/<id>/images/<idx>.<ext>) already pass through nginx +
    // our auth; double-proxying through /api/proxy/image would fail the
    // proxy's allow-list (SSRF guard) and add a useless round-trip.
    const isOwnImage = src?.startsWith('/api/articles/')
    const proxied = src
      ? isOwnImage
        ? src
        : `/api/proxy/image?url=${encodeURIComponent(src)}`
      : undefined
    // Lookup intrinsic dimensions by ORIGINAL url (the markdown-level src,
    // before proxy rewriting). When present, modern browsers use the
    // width+height attributes as an aspect-ratio hint and reserve the
    // correct vertical space even while the image is still downloading.
    const dims = useContext(ImageDimensionsContext)
    const dim = src ? dims?.[src] : undefined
    return (
      <img
        src={proxied}
        alt={alt ?? ''}
        loading="lazy"
        decoding="async"
        width={dim?.[0]}
        height={dim?.[1]}
        style={{ maxWidth: '100%', height: 'auto' }}
        {...rest}
      />
    )
  },
  a: ArticleLink,
  p: ({ children, node: _node, ...rest }) => {
    const suppressedVideo = useContext(SuppressedVideoContext)
    const text = extractParagraphText(children)
    if (text) {
      const v = parsePlaceholder(text)
      if (v) {
        if (
          suppressedVideo &&
          v.platform === suppressedVideo.platform &&
          v.id === suppressedVideo.id
        ) {
          return null
        }
        return <div {...rest}><VideoEmbed {...v} /></div>
      }
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
function MarkdownArticle({ source, imageDimensions, suppressVideo }: Props) {
  const cleaned = useMemo(
    () => prepareArticleMarkdown(source),
    [source],
  )
  const articleAnchors = useMemo(() => findArticleAnchors(cleaned), [cleaned])
  const articleAnchorRemarkPlugin = useMemo(
    () => createArticleAnchorRemarkPlugin(articleAnchors),
    [articleAnchors],
  )
  const remarkPlugins = useMemo(
    () => [...REMARK_PLUGINS, articleAnchorRemarkPlugin],
    [articleAnchorRemarkPlugin],
  )
  const articleLang = useMemo(() => detectArticleLang(cleaned), [cleaned])
  const dims = imageDimensions ?? null
  return (
    <ReaderInteractionSurface articleKey={source} className="markdown-body" lang={articleLang}>
      <SuppressedVideoContext.Provider value={suppressVideo ?? null}>
        <ImageDimensionsContext.Provider value={dims}>
          <ReactMarkdown
            remarkPlugins={remarkPlugins}
            rehypePlugins={REHYPE_PLUGINS}
            components={COMPONENTS}
          >
            {cleaned}
          </ReactMarkdown>
        </ImageDimensionsContext.Provider>
      </SuppressedVideoContext.Provider>
    </ReaderInteractionSurface>
  )
}

export default memo(MarkdownArticle)
