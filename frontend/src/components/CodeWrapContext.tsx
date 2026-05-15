import { createContext } from 'react'

// Distributes the user's global "代码块自动换行" preference into the
// memoized MarkdownArticle subtree. The CodeBlock component (registered
// as the module-scoped pre override in MarkdownArticle) reads this so we
// don't have to thread reader settings through props and rebuild the
// react-markdown components map on every render — that path triggers
// img remounts (see commit ec395e5).
export const CodeWrapContext = createContext<boolean>(false)
