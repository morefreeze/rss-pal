import { createContext } from 'react'

export type LinkSetContextValue = {
  candidateURLs: Set<string>        // normalized candidate URLs
  markedURLs: Set<string>            // subset user has clicked the icon for
  alreadyFetchedURLs: Set<string>    // candidates already turned into children
  normalize: (href: string) => string  // article-base-aware normalizer
  onToggleMark: (url: string) => void  // toggles a URL in markedURLs
}

// null means the article either isn't in link_set mode or the provider
// hasn't mounted yet — consumers must handle null and render the plain link.
export const LinkSetContext = createContext<LinkSetContextValue | null>(null)
