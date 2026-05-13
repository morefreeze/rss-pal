import ReactMarkdown from 'react-markdown'
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

// Rewrites <img src="..."> to go through the backend proxy so hotlink-
// protected sites (WeChat, Zhihu) actually render. Author/profile avatars
// are dropped entirely (see isAvatarImg). LaTeX math via remark-math +
// rehype-katex; Jina Reader's shadow duplicate is removed via stripMathShadow
// before parsing. External links open in a new tab.
export default function MarkdownArticle({ source }: Props) {
  const cleaned = flattenImageAltBlankLines(escapeAmbiguousMathDollars(stripMathShadow(source)))
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeHighlight, rehypeKatex]}
        components={{
          img: ({ src, alt, ...rest }) => {
            if (isAvatarImg(src, alt)) return null
            const proxied = src
              ? `/api/proxy/image?url=${encodeURIComponent(src)}`
              : undefined
            // Default width/height give the browser an aspect-ratio hint
            // (4:3) so it reserves vertical space before the image loads.
            // Without this, lazy-loaded images expand from 0 → natural
            // height at load time and jolt the scroll position. CSS
            // `height: auto` lets the actual aspect ratio take over once
            // dimensions are known.
            return (
              <img
                src={proxied}
                alt={alt ?? ''}
                loading="lazy"
                decoding="async"
                width={1024}
                height={768}
                style={{ maxWidth: '100%', height: 'auto', background: 'var(--surface-2, #f3f4f6)' }}
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
        }}
      >
        {cleaned}
      </ReactMarkdown>
    </div>
  )
}
