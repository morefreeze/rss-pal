import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { isLoggedIn, getMe, getUser, logout } from './api/client'
import LoginPage from './pages/LoginPage'
import RegisterPage from './pages/RegisterPage'
import FeedListPage from './pages/FeedListPage'
import ArticleListPage from './pages/ArticleListPage'
import ArticlePage from './pages/ArticlePage'
import InsightsPage from './pages/InsightsPage'
import StatsPage from './pages/StatsPage'
import SettingsPage from './pages/SettingsPage'
import SharePage from './pages/SharePage'
import RecommendedPage from './pages/RecommendedPage'
import WeeklyPage from './pages/WeeklyPage'
import DailyPage from './pages/DailyPage'
import BriefingRedirect from './components/BriefingRedirect'
import FeedHealthPage from './pages/FeedHealthPage'
import ClipPage from './pages/ClipPage'
import ExtensionConfigPage from './pages/ExtensionConfigPage'
import Layout from './components/Layout'

interface User {
  id: number
  username: string
  is_admin: boolean
}

function RequireAuth({ user, onLogout }: { user: User | null; onLogout: () => void }) {
  if (!isLoggedIn()) {
    return <Navigate to="/login" replace />
  }
  return <Layout user={user} onLogout={onLogout} />
}

function App() {
  // Hydrate from the locally cached user so a flaky network on boot doesn't
  // log the user out — the JWT in localStorage is still valid, and the 401
  // path is already handled by the response interceptor in api/client.
  const [user, setUser] = useState<User | null>(() => (isLoggedIn() ? getUser() : null))

  useEffect(() => {
    if (!isLoggedIn()) return
    getMe()
      .then(u => setUser(u))
      .catch(() => {
        // Transient errors (network, 5xx, DNS) — keep the cached session.
        // The interceptor logs out on a real 401.
      })
  }, [])

  const handleLogout = () => {
    logout()
    setUser(null)
    window.location.href = '/login'
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage onLogin={setUser} />} />
        <Route path="/register" element={<RegisterPage onLogin={setUser} />} />
        <Route path="/share/:token" element={<SharePage />} />
        <Route path="/extension-config" element={<ExtensionConfigPage />} />
        <Route element={<RequireAuth user={user} onLogout={handleLogout} />}>
          <Route index element={<Navigate to="/articles" replace />} />
          <Route path="feeds" element={<FeedListPage />} />
          <Route path="feeds/health" element={<FeedHealthPage />} />
          <Route path="recommended" element={<RecommendedPage />} />
          <Route path="briefing" element={<BriefingRedirect />} />
          <Route path="daily" element={<DailyPage />} />
          <Route path="weekly" element={<WeeklyPage />} />
          <Route path="articles" element={<ArticleListPage />} />
          <Route path="articles/:id" element={<ArticlePage />} />
          <Route path="clip" element={<ClipPage />} />
          {/* Back-compat: bookmarks of the old /saved view land on /articles
              with the 已保存 checkbox pre-ticked. ArticleListPage handles the
              query param and strips it from the URL. */}
          <Route path="saved" element={<Navigate to="/articles?saved=1" replace />} />
          <Route path="insights" element={<InsightsPage />} />
          <Route path="stats" element={<StatsPage />} />
          <Route path="settings" element={<SettingsPage user={user} />} />
        </Route>
        <Route path="*" element={<Navigate to="/articles" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
