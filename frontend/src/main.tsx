import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { applyInitialTheme } from './util/theme'
import './index.css'

applyInitialTheme()
// Reading mode is a per-session toggle, never persisted. If any stale class
// survived a prior render path (HMR, navigation), strip it before the first
// paint so the article page never briefly flashes into reading mode.
const clearReadingClasses = () => {
  document.body.classList.remove('reading-mode-active', 'reading-chrome-visible')
}
clearReadingClasses()
// bfcache restores the previous DOM (classes and all) without re-running
// this module, so we also strip on pageshow — handles the case where the
// user hit Back/Forward (or some browsers' "refresh") and the persisted
// `reading-mode-active` class would otherwise flash through.
window.addEventListener('pageshow', clearReadingClasses)

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
