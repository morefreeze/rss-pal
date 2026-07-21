export type ReaderLinkTarget = {
  url: string
  title: string
  element: HTMLAnchorElement
}

export type ReaderContextTarget =
  | {
    kind: 'selection-links'
    links: ReaderLinkTarget[]
    anchorRect: DOMRect
  }
  | {
    kind: 'long-press-link'
    links: [ReaderLinkTarget]
    anchorRect: DOMRect
  }

export type ReaderContextAction = {
  id: string
  label: string
  disabled?: boolean
  run: () => void | Promise<void>
}

export type ReaderActionContextValue = {
  normalizeLink(href: string): string | null
  getLinkState(url: string): 'draft' | 'fetched' | null
  getActions(target: ReaderContextTarget): ReaderContextAction[]
  onLinkDiscovered?(link: Pick<ReaderLinkTarget, 'url' | 'title'>): void
}
