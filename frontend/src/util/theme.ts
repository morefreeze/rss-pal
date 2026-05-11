import { useEffect, useState } from 'react'

export type Theme = 'paper' | 'quiet' | 'pearl' | 'night'

const STORAGE_KEY = 'rsspal:theme'
const DEFAULT: Theme = 'paper'
const EVENT = 'rsspal-theme-change'

const VALID: Record<Theme, true> = { paper: true, quiet: true, pearl: true, night: true }

function isTheme(v: unknown): v is Theme {
  return typeof v === 'string' && Object.prototype.hasOwnProperty.call(VALID, v)
}

export function getTheme(): Theme {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (isTheme(raw)) return raw
    // Best-effort one-time migration from the legacy reader-mode bgTheme key
    // so users who picked sepia/dark/gray there don't lose continuity. Legacy
    // 'green' and 'default' have no equivalents and fall through to DEFAULT.
    const legacyRaw = localStorage.getItem('rsspal:reader-settings')
    if (legacyRaw) {
      try {
        const parsed = JSON.parse(legacyRaw) as { bgTheme?: string }
        let migrated: Theme | undefined
        if (parsed.bgTheme === 'sepia') migrated = 'paper'
        else if (parsed.bgTheme === 'dark') migrated = 'night'
        else if (parsed.bgTheme === 'gray') migrated = 'pearl'
        if (migrated) {
          try { localStorage.setItem(STORAGE_KEY, migrated) } catch { /* ignore */ }
          return migrated
        }
      } catch { /* ignore */ }
    }
  } catch { /* ignore */ }
  return DEFAULT
}

export function setTheme(t: Theme): void {
  document.body.setAttribute('data-theme', t)
  try { localStorage.setItem(STORAGE_KEY, t) } catch { /* ignore */ }
  window.dispatchEvent(new CustomEvent(EVENT, { detail: t }))
}

export function applyInitialTheme(): void {
  document.body.setAttribute('data-theme', getTheme())
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, setThemeState] = useState<Theme>(() => getTheme())
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<Theme>).detail
      if (isTheme(detail)) setThemeState(detail)
    }
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY && isTheme(e.newValue)) setThemeState(e.newValue)
    }
    window.addEventListener(EVENT, handler)
    window.addEventListener('storage', onStorage)
    return () => {
      window.removeEventListener(EVENT, handler)
      window.removeEventListener('storage', onStorage)
    }
  }, [])
  return [theme, setTheme]
}
