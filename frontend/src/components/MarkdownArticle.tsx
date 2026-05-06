import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import rehypeHighlight from 'rehype-highlight'
import 'highlight.js/styles/github.css'

type Props = {
  source: string
}

// Rewrites <img src="..."> to go through the backend proxy so hotlink-
// protected sites (WeChat, Zhihu) actually render. Also forces external
// links to open in a new tab.
export default function MarkdownArticle({ source }: Props) {
  return (
    <div className="markdown-body">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          img: ({ src, alt, ...rest }) => {
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
        }}
      >
        {source}
      </ReactMarkdown>
    </div>
  )
}
