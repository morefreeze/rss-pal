const digestTag = 'rss-pal-unread'

self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting())
})

self.addEventListener('push', (event) => {
  event.waitUntil(showDigest(event))
})

async function showDigest(event) {
  let message
  try {
    message = event.data?.json()
  } catch {
    message = undefined
  }
  const count = message?.app_badge
  await Promise.all([
    self.registration.showNotification('RSS Pal', {
      body: message?.notification?.body || '查看未读文章',
      icon: '/apple-touch-icon.png',
      tag: digestTag,
    }),
    Number.isSafeInteger(count) && count >= 0 && self.navigator.setAppBadge
      ? self.navigator.setAppBadge(count).catch(() => {})
      : undefined,
  ])
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  for (const client of windows) {
    if (client.visibilityState === 'visible') client.postMessage('rss-pal-refresh-unread')
  }
}

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil(openArticles())
})

async function openArticles() {
  const target = new URL('/articles', self.location.origin).href
  const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
  const existing = windows.find((client) => new URL(client.url).origin === self.location.origin)
  if (existing) {
    try {
      const articles = await existing.navigate(target)
      if (articles) {
        await articles.focus()
        return
      }
    } catch {
      // The window may have closed during navigation.
    }
  }
  await self.clients.openWindow(target)
}
