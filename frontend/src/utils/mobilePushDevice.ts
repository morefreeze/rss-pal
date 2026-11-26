const enrolledUserKey = 'rsspal:mobile-push-user'
const digestTag = 'rss-pal-unread'

export function isInstalledApp(): boolean {
  return window.matchMedia?.('(display-mode: standalone)').matches === true ||
    (navigator as Navigator & { standalone?: boolean }).standalone === true
}

export function supportsMobilePush(): boolean {
  return isInstalledApp() && 'serviceWorker' in navigator &&
    'PushManager' in window && 'Notification' in window
}

export async function prepareMobilePush(): Promise<ServiceWorkerRegistration> {
  await navigator.serviceWorker.register('/push-worker.js', { updateViaCache: 'none' })
  return navigator.serviceWorker.ready
}

export function syncMobileBadge(userID: number, count: number): void {
  if (isInstalledApp() && localStorage.getItem(enrolledUserKey) === String(userID)) {
    void navigator.setAppBadge?.(count).catch(() => {})
  }
}

export function setMobilePushEnrollment(userID: number): void {
  localStorage.setItem(enrolledUserKey, String(userID))
  window.dispatchEvent(new Event('refresh-unread'))
}

export async function clearMobilePushEnrollment(): Promise<void> {
  localStorage.removeItem(enrolledUserKey)
  await navigator.clearAppBadge?.().catch(() => {})
}

export async function closeMobilePushNotifications(registration?: ServiceWorkerRegistration): Promise<void> {
  const notifications = await registration?.getNotifications({ tag: digestTag })
  notifications?.forEach((notification) => notification.close())
}

export async function resetMobilePushDevice(): Promise<void> {
  if (!localStorage.getItem(enrolledUserKey)) return
  const clearedBadge = clearMobilePushEnrollment()
  const registration = await navigator.serviceWorker?.getRegistration()
  const subscription = await registration?.pushManager.getSubscription()
  await Promise.all([clearedBadge, subscription?.unsubscribe(), closeMobilePushNotifications(registration)])
}
