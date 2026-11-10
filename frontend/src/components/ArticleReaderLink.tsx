import type { ReactNode } from 'react'

export default function ArticleReaderLink({ href, onOpen, children, className, label }: {
  href: string
  onOpen: () => void
  children: ReactNode
  className?: string
  label?: string
}) {
  return (
    <a
      href={href}
      className={className}
      aria-label={label}
      data-article-reader-link
      onClick={event => {
        event.stopPropagation()
        if (event.defaultPrevented || event.button !== 0 ||
          event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
        event.preventDefault()
        onOpen()
      }}
    >
      {children}
    </a>
  )
}
