import {
  getInitialPopularFeedsExpanded,
  POPULAR_FEEDS_AUTO_COLLAPSE_MS,
  POPULAR_FEEDS_FIRST_SEEN_KEY,
} from '../src/utils/popularFeedsVisibility'
import { describe, it } from 'vitest'

function assertEqual<T>(actual: T, expected: T, label: string) {
  if (actual !== expected) {
    throw new Error(`${label}: expected ${String(expected)}, got ${String(actual)}`)
  }
}

class MemoryStorage {
  private readonly values = new Map<string, string>()

  constructor(initialValue?: string) {
    if (initialValue !== undefined) {
      this.values.set(POPULAR_FEEDS_FIRST_SEEN_KEY, initialValue)
    }
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value)
  }
}

describe('popular feeds visibility', () => {
it('preserves the existing visibility contracts', () => {
const now = 2_000_000_000_000

const newBrowserStorage = new MemoryStorage()
assertEqual(
  getInitialPopularFeedsExpanded(now, newBrowserStorage),
  true,
  'new browser expands popular feeds',
)
assertEqual(
  newBrowserStorage.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY),
  String(now),
  'new browser records first-seen time',
)

const recentFirstSeenAt = now - 71 * 60 * 60 * 1000
const recentStorage = new MemoryStorage(String(recentFirstSeenAt))
assertEqual(
  getInitialPopularFeedsExpanded(now, recentStorage),
  true,
  '71-hour-old browser expands popular feeds',
)
assertEqual(
  recentStorage.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY),
  String(recentFirstSeenAt),
  'recent first-seen time is not overwritten',
)

const boundaryStorage = new MemoryStorage(String(now - POPULAR_FEEDS_AUTO_COLLAPSE_MS))
assertEqual(
  getInitialPopularFeedsExpanded(now, boundaryStorage),
  false,
  'browser collapses popular feeds at the exact threshold',
)

const expiredStorage = new MemoryStorage(String(now - 73 * 60 * 60 * 1000))
assertEqual(
  getInitialPopularFeedsExpanded(now, expiredStorage),
  false,
  '73-hour-old browser collapses popular feeds',
)

for (const invalidValue of ['not-a-number', '0', '-1', String(now + 1)]) {
  const invalidStorage = new MemoryStorage(invalidValue)
  assertEqual(
    getInitialPopularFeedsExpanded(now, invalidStorage),
    true,
    `invalid first-seen value ${invalidValue} expands popular feeds`,
  )
  assertEqual(
    invalidStorage.getItem(POPULAR_FEEDS_FIRST_SEEN_KEY),
    String(now),
    `invalid first-seen value ${invalidValue} is replaced`,
  )
}

const readFailureStorage = {
  getItem(): string | null {
    throw new Error('read failed')
  },
  setItem(): void {},
}
assertEqual(
  getInitialPopularFeedsExpanded(now, readFailureStorage),
  true,
  'storage read failure expands popular feeds',
)

const writeFailureStorage = {
  getItem(): string | null {
    return null
  },
  setItem(): void {
    throw new Error('write failed')
  },
}
assertEqual(
  getInitialPopularFeedsExpanded(now, writeFailureStorage),
  true,
  'storage write failure expands popular feeds',
)

})
})
