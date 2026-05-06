import { useEffect, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import MarkdownArticle from './MarkdownArticle'
import ReaderSettingsPanel from './ReaderSettingsPanel'
import type {
  ReaderBgTheme,
  ReaderFontFamily,
} from '../hooks/useReaderSettings'

type ArticleLite = {
  title: string
  url: string
  published_at: string | null
  word_count: number
  reading_minutes: number
  content: string
  summary_brief: string
  summary_detailed: string
}

type Props = {
  article: ArticleLite
  fontSize: number
  fontFamily: ReaderFontFamily
  bgTheme: ReaderBgTheme
  onExit: () => void
  onFontSize: (n: number) => void
  onFontFamily: (f: ReaderFontFamily) => void
  onBgTheme: (b: ReaderBgTheme) => void
}

export default function ReadingLayout(props: Props) {
  const { article, fontSize, fontFamily, bgTheme, onExit } = props

  // Apply theme on <body> so the entire viewport adopts the bg color.
  useEffect(() => {
    const prev = document.body.getAttribute('data-reader-bg')
    document.body.setAttribute('data-reader-bg', bgTheme)
    document.body.classList.add('reading-mode-active')
    return () => {
      if (prev !== null) document.body.setAttribute('data-reader-bg', prev)
      else document.body.removeAttribute('data-reader-bg')
      document.body.classList.remove('reading-mode-active')
    }
  }, [bgTheme])

  const [summaryOpen, setSummaryOpen] = useState(false)

  const fmtDate = (s: string | null) => s ? new Date(s).toLocaleString('zh-CN') : ''
  const ff = fontFamily === 'serif'
    ? '"Source Han Serif SC", "Songti SC", serif'
    : 'system-ui, -apple-system, "PingFang SC", "Microsoft YaHei", sans-serif'

  return (
    <div className="reading-layout" style={{ fontFamily: ff }}>
      <div className="reading-toolbar">
        <button className="reading-exit" onClick={onExit} title="退出阅读模式 (Esc / r)">← 退出阅读模式</button>
      </div>

      <article className="reading-article" style={{ fontSize }}>
        <h1 className="reading-title">{article.title}</h1>
        <div className="reading-meta">
          <span>{fmtDate(article.published_at)}</span>
          {article.word_count > 0 && <span> · {article.word_count} 字</span>}
          {article.reading_minutes > 0 && <span> · 约 {article.reading_minutes} 分钟</span>}
          <span> · </span>
          <a href={article.url} target="_blank" rel="noopener noreferrer">原文链接</a>
        </div>

        {(article.summary_brief || article.summary_detailed) && (
          <div className="reading-summary">
            <button className="reading-summary-toggle" onClick={() => setSummaryOpen(o => !o)}>
              {summaryOpen ? '▼' : '▶'} AI 摘要
            </button>
            {summaryOpen && (
              <div className="reading-summary-body">
                {article.summary_brief && <ReactMarkdown>{article.summary_brief}</ReactMarkdown>}
                {article.summary_detailed && (
                  <>
                    <hr />
                    <ReactMarkdown>{article.summary_detailed}</ReactMarkdown>
                  </>
                )}
              </div>
            )}
          </div>
        )}

        {article.content
          ? <MarkdownArticle source={article.content} />
          : <div className="text-muted">暂无内容</div>
        }
      </article>

      <ReaderSettingsPanel
        fontSize={fontSize}
        fontFamily={fontFamily}
        bgTheme={bgTheme}
        onFontSize={props.onFontSize}
        onFontFamily={props.onFontFamily}
        onBgTheme={props.onBgTheme}
      />
    </div>
  )
}
