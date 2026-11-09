import { useEffect, useRef } from 'react'
import { recordReadingHistory, type ReadingHistoryReader } from '../api/client'

export function useRecordReadingHistory(kind: ReadingHistoryReader, articleID: number | undefined) {
  const lastOpened = useRef<number>()
  useEffect(() => {
    if (lastOpened.current === articleID) return
    lastOpened.current = articleID
    if (articleID !== undefined) {
      void recordReadingHistory(kind, articleID).catch(() => {})
    }
  }, [kind, articleID])
}
