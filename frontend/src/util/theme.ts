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
    // Best-effort migration from the legacy reader-mode bgTheme key so
    // users who picked sepia/dark there don't lose continuity.
    const legacyRaw = localStorage.getItem('rsspal:reader-settings')
    if (legacyRaw) {
      try {
        const parsed = JSON.parse(legacyRaw) as { bgTheme?: string }
        if (parsed.bgTheme === 'sepia') return 'paper'
        if (parsed.bgTheme === 'dark') return 'night'
        if (parsed.bgTheme === 'gray') return 'pearl'
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
