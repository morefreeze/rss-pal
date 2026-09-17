import { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { useBreakpoint } from '../hooks/useBreakpoint'
import './AdminLayout.css'

export default function AdminLayout({ user }: { user: { is_admin: boolean } | null }) {
  const [open, setOpen] = useState(false)
  const desktop = useBreakpoint() === 'desktop'
  const { pathname } = useLocation()
  useEffect(() => { setOpen(false) }, [pathname])
  if (!user?.is_admin) return <div className="card">仅管理员可访问后台管理</div>
  return <div className="admin-workspace">
    <aside className="admin-sidebar" aria-label="后台管理">
      <h2>后台管理</h2>
      {!desktop && <button className="secondary admin-menu-toggle" aria-expanded={open} aria-controls="admin-sidebar-nav" onClick={() => setOpen(value => !value)}>后台菜单 {open ? '▴' : '▾'}</button>}
      {(desktop || open) && <nav id="admin-sidebar-nav" aria-label="后台导航">
        <NavLink to="/admin/feed-catalog">📚 公共推荐目录</NavLink>
        <NavLink to="/admin/monitoring">🛡️ 运行监控</NavLink>
      </nav>}
    </aside>
    <section className="admin-content" aria-label="后台内容"><Outlet /></section>
  </div>
}
