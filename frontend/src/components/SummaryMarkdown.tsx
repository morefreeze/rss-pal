import { useMemo } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

type Props = {
  source: string
}

const REMARK_PLUGINS = [remarkGfm]

export function normalizeSummaryMarkdown(source: string): string {
  return source.replace(/(^|\n)([ \t]*)[•▸]\s+/g, '$1$2- ')
}

export default function SummaryMarkdown({ source }: Props) {
  const normalized = useMemo(() => normalizeSummaryMarkdown(source), [source])
  return (
    <ReactMarkdown remarkPlugins={REMARK_PLUGINS}>
      {normalized}
    </ReactMarkdown>
  )
}
