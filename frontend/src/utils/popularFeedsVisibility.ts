export const POPULAR_FEEDS_FIRST_SEEN_KEY = 'rsspal:popular-feeds:first-seen-at'
export const POPULAR_FEEDS_AUTO_COLLAPSE_MS = 3 * 24 * 60 * 60 * 1000

export interface PopularFeedsStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

export function getInitialPopularFeedsExpanded(
  now = Date.now(),
  storage?: PopularFeedsStorage,
): boolean {
  try {
    const popularFeedsStorage = storage ?? window.localStorage
    const storedFirstSeenAt = popularFeedsStorage.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY)

    if (storedFirstSeenAt === null) {
      popularFeedsStorage.setItem(POPULAR_FEEDS_FIRST_SEEN_KEY, String(now))
      return true
    }

    const firstSeenAt = Number(storedFirstSeenAt)
    if (!Number.isFinite(firstSeenAt) || firstSeenAt <= 0 || firstSeenAt > now) {
      popularFeedsStorage.setItem(POPULAR_FEEDS_FIRST_SEEN_KEY, String(now))
      return true
    }

    return now - firstSeenAt < POPULAR_FEEDS_AUTO_COLLAPSE_MS
  } catch {
    return true
  }
}
