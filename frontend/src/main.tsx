import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App'
import { applyInitialTheme } from './util/theme'
import './index.css'

applyInitialTheme()

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
