import { useCallback, useEffect, useState } from 'react'

export type ReaderMode = 'normal' | 'reading'
export type ReaderFontFamily = 'sans' | 'serif'

// Persisted settings. `mode` is intentionally NOT in here — reading mode is
// a per-session toggle. Background palette also lives outside this hook
// now; the global theme (util/theme.ts) provides the bg/fg colors and the
// reading view simply inherits them.
export type ReaderSettings = {
  fontSize: number      // 12..24, step 1
  fontFamily: ReaderFontFamily
  confettiEnabled: boolean
}

const STORAGE_KEY = 'rsspal:reader-settings'

const DEFAULTS: ReaderSettings = {
  fontSize: 16,
  fontFamily: 'sans',
  confettiEnabled: true,
}

function load(): ReaderSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULTS
    const parsed = JSON.parse(raw) as Partial<ReaderSettings>
    return {
      fontSize: clampFont(parsed.fontSize ?? DEFAULTS.fontSize),
      fontFamily: parsed.fontFamily === 'serif' ? 'serif' : 'sans',
      confettiEnabled: parsed.confettiEnabled !== false,
    }
  } catch {
    return DEFAULTS
  }
}

function clampFont(n: number): number {
  if (!Number.isFinite(n)) return DEFAULTS.fontSize
  return Math.max(12, Math.min(24, Math.round(n)))
}

export function useReaderSettings() {
  const [settings, setSettings] = useState<ReaderSettings>(() => load())
  const [mode, setModeState] = useState<ReaderMode>('normal')

  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(settings)) } catch {}
  }, [settings])

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setSettings(load())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const setMode = useCallback((m: ReaderMode) => setModeState(m), [])
  const toggleMode = useCallback(() =>
    setModeState(m => m === 'reading' ? 'normal' : 'reading'), [])
  const setFontSize = useCallback((fontSize: number) =>
    setSettings(s => ({ ...s, fontSize: clampFont(fontSize) })), [])
  const setFontFamily = useCallback((fontFamily: ReaderFontFamily) =>
    setSettings(s => ({ ...s, fontFamily })), [])
  const setConfettiEnabled = useCallback((confettiEnabled: boolean) =>
    setSettings(s => ({ ...s, confettiEnabled })), [])

  return {
    ...settings,
    mode,
    setMode,
    toggleMode,
    setFontSize,
    setFontFamily,
    setConfettiEnabled,
  }
}
