import { useEffect } from 'react'
import { buildDocumentTitle } from '../utils/pageTitle'

export function useDocumentTitle(pageTitle: string): void {
  useEffect(() => {
    document.title = buildDocumentTitle(pageTitle)
  }, [pageTitle])
}
