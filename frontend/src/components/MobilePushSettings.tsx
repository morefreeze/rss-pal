import { useEffect, useState } from 'react'
import { api, getUser } from '../api/client'
import { clearMobilePushEnrollment, closeMobilePushNotifications, isInstalledApp, prepareMobilePush, setMobilePushEnrollment, supportsMobilePush } from '../utils/mobilePushDevice'

const settingsPath = '/settings/mobile-push'

function decodePublicKey(key: string): ArrayBuffer {
  const raw = atob(key.replace(/-/g, '+').replace(/_/g, '/'))
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
  return bytes.buffer
}

export default function MobilePushSettings({ userID }: { userID: number }) {
  const supported = supportsMobilePush()
  const [device, setDevice] = useState<{ registration: ServiceWorkerRegistration; key: ArrayBuffer }>()
  const [subscription, setSubscription] = useState<PushSubscription | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [reload, setReload] = useState(0)

  useEffect(() => {
    if (!supported) return
    let cancelled = false
    const load = async () => {
      const registration = await prepareMobilePush()
      let current = await registration.pushManager.getSubscription()
      const { data } = await api.post<{ active: boolean; public_key: string }>(settingsPath + '/status', {
        endpoint: current?.endpoint,
      })
      if (cancelled) return
      if (current && !data.active) {
        await current.unsubscribe()
        current = null
      }
      if (cancelled) return
      if (current) setMobilePushEnrollment(userID)
      else await clearMobilePushEnrollment()
      if (cancelled) return
      setSubscription(current)
      setDevice({ registration, key: decodePublicKey(data.public_key) })
    }
    void load().catch(() => {
      if (!cancelled) setError('无法读取通知设置，请重试。')
    })
    return () => { cancelled = true }
  }, [supported, userID, reload])

  async function toggle() {
    if (!device || busy) return
    setBusy(true)
    setError('')
    try {
      if (subscription) {
        await api.delete(settingsPath, { data: { endpoint: subscription.endpoint } })
        setSubscription(null)
        await clearMobilePushEnrollment()
        await subscription.unsubscribe().catch(() => {})
        await closeMobilePushNotifications(device.registration).catch(() => {})
      } else {
        // Start subscribe from the click handler so the browser can request permission.
        const next = await device.registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: device.key,
        })
        try {
          if (getUser()?.id !== userID) throw new Error('Account changed')
          await api.put(settingsPath, next.toJSON())
          if (getUser()?.id !== userID) throw new Error('Account changed')
        } catch (err) {
          await next.unsubscribe().catch(() => {})
          throw err
        }
        setSubscription(next)
        setMobilePushEnrollment(userID)
      }
    } catch {
      setError(Notification.permission === 'denied'
        ? '系统通知已关闭，请在手机设置中允许 RSS Pal 通知。'
        : '无法更新通知设置，请重试。')
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card mb-2">
      <h3 className="mb-2">新文章通知</h3>
      {supported ? (
        <>
          <p className="text-sm mb-2">每 3 小时合并通知一次，有新增未读文章时才推送。支持数字角标的设备会显示总未读数。</p>
          {error && <p className="text-sm mb-2" role="alert">{error}</p>}
          {error && !device
            ? <button className="btn btn-sm" type="button" onClick={() => { setError(''); setReload(value => value + 1) }}>重试</button>
            : <button className="btn btn-sm" type="button" disabled={busy || !device} onClick={() => void toggle()}>
                {busy ? '正在更新…' : !device ? '正在读取…' : subscription ? '关闭此设备通知' : '开启此设备通知'}
              </button>}
        </>
      ) : (
        <p className="text-sm">{isInstalledApp()
          ? '当前浏览器或系统不支持网页推送。'
          : '在手机浏览器把 RSS Pal 添加到主屏幕，再从图标打开，即可设置通知。'}</p>
      )}
    </section>
  )
}
