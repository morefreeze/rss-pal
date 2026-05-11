import { useCallback, useEffect, useState } from 'react'

export type ReaderMode = 'normal' | 'reading'
export type ReaderFontFamily = 'sans' | 'serif'
export type ReaderBgTheme = 'default' | 'sepia' | 'green' | 'gray' | 'dark'

export type ReaderSettings = {
  mode: ReaderMode
  fontSize: number      // 12..24, step 1
  fontFamily: ReaderFontFamily
  bgTheme: ReaderBgTheme
  confettiEnabled: boolean
}

const STORAGE_KEY = 'rsspal:reader-settings'

const DEFAULTS: ReaderSettings = {
  mode: 'normal',
  fontSize: 16,
  fontFamily: 'sans',
  bgTheme: 'default',
  confettiEnabled: true,
}

function load(): ReaderSettings {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULTS
    const parsed = JSON.parse(raw) as Partial<ReaderSettings>
    return {
      mode: parsed.mode === 'reading' ? 'reading' : 'normal',
      fontSize: clampFont(parsed.fontSize ?? DEFAULTS.fontSize),
      fontFamily: parsed.fontFamily === 'serif' ? 'serif' : 'sans',
      bgTheme: isTheme(parsed.bgTheme) ? parsed.bgTheme : 'default',
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

function isTheme(v: unknown): v is ReaderBgTheme {
  return v === 'default' || v === 'sepia' || v === 'green' || v === 'gray' || v === 'dark'
}

export function useReaderSettings() {
  const [settings, setSettings] = useState<ReaderSettings>(() => load())

  // Persist on every change
  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(settings)) } catch {}
  }, [settings])

  // Sync across tabs
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setSettings(load())
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
  }, [])

  const setMode = useCallback((mode: ReaderMode) => setSettings(s => ({ ...s, mode })), [])
  const setFontSize = useCallback((fontSize: number) =>
    setSettings(s => ({ ...s, fontSize: clampFont(fontSize) })), [])
  const setFontFamily = useCallback((fontFamily: ReaderFontFamily) =>
    setSettings(s => ({ ...s, fontFamily })), [])
  const setBgTheme = useCallback((bgTheme: ReaderBgTheme) =>
    setSettings(s => ({ ...s, bgTheme })), [])
  const setConfettiEnabled = useCallback((confettiEnabled: boolean) =>
    setSettings(s => ({ ...s, confettiEnabled })), [])
  const toggleMode = useCallback(() =>
    setSettings(s => ({ ...s, mode: s.mode === 'reading' ? 'normal' : 'reading' })), [])

  return {
    ...settings,
    setMode,
    toggleMode,
    setFontSize,
    setFontFamily,
    setBgTheme,
    setConfettiEnabled,
  }
}
