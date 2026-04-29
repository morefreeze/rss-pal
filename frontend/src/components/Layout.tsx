import { NavLink, Outlet } from 'react-router-dom'
import { logout } from '../api/client'

interface LayoutProps {
  user: { id: number; username: string; is_admin: boolean } | null
  onLogout: () => void
}

export default function Layout({ user, onLogout }: LayoutProps) {
  const handleLogout = () => {
    logout()
    onLogout()
  }

  return (
    <div>
      <header className="flex-between mb-2">
        <h1>RSS Pal</h1>
        <nav className="flex gap-2" style={{ alignItems: 'center' }}>
          <NavLink to="/articles" className={({ isActive }) => isActive ? 'active' : ''}>
            文章
          </NavLink>
          <NavLink to="/feeds" className={({ isActive }) => isActive ? 'active' : ''}>
            订阅
          </NavLink>
          <NavLink to="/insights" className={({ isActive }) => isActive ? 'active' : ''}>
            洞察
          </NavLink>
          <NavLink to="/stats" className={({ isActive }) => isActive ? 'active' : ''}>
            统计
          </NavLink>
          <NavLink to="/settings" className={({ isActive }) => isActive ? 'active' : ''}>
            设置
          </NavLink>
          <span className="text-muted text-sm">{user?.username}</span>
          <button className="secondary" onClick={handleLogout}>登出</button>
        </nav>
      </header>
      <main>
        <Outlet />
      </main>
    </div>
  )
}
