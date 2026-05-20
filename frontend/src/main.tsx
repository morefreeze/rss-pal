import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { applyInitialTheme } from './util/theme'
import './index.css'

applyInitialTheme()
// Reading mode is a per-session toggle, never persisted. If any stale class
// survived a prior render path (HMR, navigation), strip it before the first
// paint so the article page never briefly flashes into reading mode.
document.body.classList.remove('reading-mode-active', 'reading-chrome-visible')

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
