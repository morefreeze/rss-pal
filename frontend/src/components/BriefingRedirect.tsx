import { useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { getBriefingLastTab } from '../api/client'

export default function BriefingRedirect() {
  const navigate = useNavigate()
  useEffect(() => {
    let cancelled = false
    getBriefingLastTab()
      .then(({ tab }) => {
        if (cancelled) return
        navigate('/' + tab, { replace: true })
      })
      .catch(() => {
        if (cancelled) return
        navigate('/daily', { replace: true })
      })
    return () => { cancelled = true }
  }, [navigate])
  return (
    <div className="card">加载中…</div>
  )
}
