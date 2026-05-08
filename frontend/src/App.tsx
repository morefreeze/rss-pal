import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { useState, useEffect } from 'react'
import { isLoggedIn, getMe, logout } from './api/client'
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
import FeedHealthPage from './pages/FeedHealthPage'
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
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(isLoggedIn())

  useEffect(() => {
    if (!isLoggedIn()) {
      setLoading(false)
      return
    }
    getMe()
      .then(u => setUser(u))
      .catch(() => {
        logout()
      })
      .finally(() => setLoading(false))
  }, [])

  const handleLogout = () => {
    logout()
    setUser(null)
    window.location.href = '/login'
  }

  if (loading) {
    return <div className="card">Loading...</div>
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage onLogin={setUser} />} />
        <Route path="/register" element={<RegisterPage onLogin={setUser} />} />
        <Route path="/share/:token" element={<SharePage />} />
        <Route element={<RequireAuth user={user} onLogout={handleLogout} />}>
          <Route index element={<Navigate to="/articles" replace />} />
          <Route path="feeds" element={<FeedListPage />} />
          <Route path="feeds/health" element={<FeedHealthPage />} />
          <Route path="recommended" element={<RecommendedPage />} />
          <Route path="weekly" element={<WeeklyPage />} />
          <Route path="articles" element={<ArticleListPage />} />
          <Route path="articles/:id" element={<ArticlePage />} />
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
