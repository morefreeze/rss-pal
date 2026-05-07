import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { Article, getPlayback, putPlayback } from '../api/client'

type Speed = 1 | 1.25 | 1.5 | 1.75 | 2

interface PlayerState {
  articleId: number | null
  title: string
  feedTitle: string
  src: string
  duration: number
  position: number
  playing: boolean
  speed: Speed
  loading: boolean
  error: string | null
}

interface PlayerActions {
  playArticle(article: Article): Promise<void>
  toggle(): void
  seek(sec: number): void
  skip(deltaSec: number): void
  setSpeed(s: Speed): void
  close(): void
}

type PlayerContextValue = PlayerState & PlayerActions & { audioRef: React.RefObject<HTMLAudioElement> }

const PlayerContext = createContext<PlayerContextValue | null>(null)

export function usePlayer(): PlayerContextValue {
  const ctx = useContext(PlayerContext)
  if (!ctx) throw new Error('usePlayer must be used inside <PlayerProvider>')
  return ctx
}

const initial: PlayerState = {
  articleId: null,
  title: '',
  feedTitle: '',
  src: '',
  duration: 0,
  position: 0,
  playing: false,
  speed: 1,
  loading: false,
  error: null,
}

export function PlayerProvider({ children }: { children: React.ReactNode }) {
  const audioRef = useRef<HTMLAudioElement>(null)
  const pendingResumeListenerRef = useRef<(() => void) | null>(null)
  const [state, setState] = useState<PlayerState>(initial)
  const stateRef = useRef(state)
  stateRef.current = state

  // Flush latest position to backend. Safe to call any time; no-ops when no article.
  const flush = useCallback(async (overrides?: { position?: number; isCompleted?: boolean }) => {
    const s = stateRef.current
    if (!s.articleId) return
    const position = overrides?.position ?? s.position
    const isCompleted = overrides?.isCompleted ?? false
    try {
      await putPlayback(s.articleId, { position_seconds: Math.floor(position), is_completed: isCompleted })
    } catch {
      // ignore — next 10s tick will retry
    }
  }, [])

  // Periodic flush every 10s while playing.
  useEffect(() => {
    if (!state.playing) return
    const id = window.setInterval(() => { flush() }, 10000)
    return () => window.clearInterval(id)
  }, [state.playing, flush])

  // Bind <audio> events.
  useEffect(() => {
    const el = audioRef.current
    if (!el) return
    const onLoaded = () => setState(s => ({ ...s, duration: el.duration || s.duration }))
    const onTime = () => setState(s => ({ ...s, position: el.currentTime }))
    const onPlay = () => setState(s => ({ ...s, playing: true, loading: true }))
    const onPlaying = () => setState(s => ({ ...s, playing: true, loading: false }))
    const onWaiting = () => setState(s => ({ ...s, loading: true }))
    const onPause = () => { setState(s => ({ ...s, playing: false, loading: false })); flush() }
    const onEnded = () => {
      setState(s => ({ ...s, playing: false, loading: false }))
      flush({ position: stateRef.current.duration, isCompleted: true })
    }
    const onError = () => setState(s => ({ ...s, error: '无法加载音频', loading: false, playing: false }))
    el.addEventListener('loadedmetadata', onLoaded)
    el.addEventListener('timeupdate', onTime)
    el.addEventListener('play', onPlay)
    el.addEventListener('playing', onPlaying)
    el.addEventListener('waiting', onWaiting)
    el.addEventListener('pause', onPause)
    el.addEventListener('ended', onEnded)
    el.addEventListener('error', onError)
    return () => {
      el.removeEventListener('loadedmetadata', onLoaded)
      el.removeEventListener('timeupdate', onTime)
      el.removeEventListener('play', onPlay)
      el.removeEventListener('playing', onPlaying)
      el.removeEventListener('waiting', onWaiting)
      el.removeEventListener('pause', onPause)
      el.removeEventListener('ended', onEnded)
      el.removeEventListener('error', onError)
    }
  }, [flush])

  const clearPendingResume = useCallback(() => {
    const el = audioRef.current
    const listener = pendingResumeListenerRef.current
    if (el && listener) {
      el.removeEventListener('loadedmetadata', listener)
    }
    pendingResumeListenerRef.current = null
  }, [])

  const playArticle = useCallback(async (article: Article) => {
    if (!article.media_url) return
    const el = audioRef.current
    if (!el) return

    // If switching to a different article, flush the old one first.
    if (stateRef.current.articleId && stateRef.current.articleId !== article.id) {
      await flush()
    }

    let resumeAt = 0
    try {
      const p = await getPlayback(article.id)
      resumeAt = p.is_completed ? 0 : p.position_seconds
    } catch {
      // ok, start from 0
    }

    setState({
      articleId: article.id,
      title: article.title,
      feedTitle: article.feed_title || '',
      src: article.media_url,
      duration: article.media_duration_seconds || 0,
      position: resumeAt,
      playing: false,
      speed: stateRef.current.speed,
      loading: true,
      error: null,
    })

    // Drop any previous pending resume seek before binding a new one — under
    // rapid switches the old listener would otherwise fire against the new src.
    clearPendingResume()

    el.src = article.media_url
    el.playbackRate = stateRef.current.speed
    // Wait for the metadata before seeking — otherwise the seek is dropped.
    const playFromResume = () => {
      pendingResumeListenerRef.current = null
      el.currentTime = resumeAt
      el.play().catch(() => { setState(s => ({ ...s, loading: false, playing: false })) })
      el.removeEventListener('loadedmetadata', playFromResume)
    }
    pendingResumeListenerRef.current = playFromResume
    el.addEventListener('loadedmetadata', playFromResume)
    el.load()
  }, [flush, clearPendingResume])

  const toggle = useCallback(() => {
    const el = audioRef.current
    if (!el || !stateRef.current.articleId) return
    if (el.paused) el.play().catch(() => { setState(s => ({ ...s, loading: false, playing: false })) })
    else el.pause()
  }, [])

  const seek = useCallback((sec: number) => {
    const el = audioRef.current
    if (!el) return
    el.currentTime = Math.max(0, sec)
  }, [])

  const skip = useCallback((delta: number) => {
    const el = audioRef.current
    if (!el) return
    el.currentTime = Math.max(0, Math.min(el.duration || Infinity, el.currentTime + delta))
  }, [])

  const setSpeed = useCallback((s: Speed) => {
    const el = audioRef.current
    if (el) el.playbackRate = s
    setState(prev => ({ ...prev, speed: s }))
  }, [])

  const close = useCallback(() => {
    clearPendingResume()
    const el = audioRef.current
    if (el) {
      el.pause()
      el.removeAttribute('src')
      el.load()
    }
    flush()
    setState(initial)
  }, [flush, clearPendingResume])

  // MediaSession (lock-screen / hardware keys).
  useEffect(() => {
    if (!('mediaSession' in navigator)) return
    if (!state.articleId) {
      navigator.mediaSession.metadata = null
      return
    }
    navigator.mediaSession.metadata = new MediaMetadata({
      title: state.title,
      artist: state.feedTitle,
    })
    navigator.mediaSession.setActionHandler('play', toggle)
    navigator.mediaSession.setActionHandler('pause', toggle)
    navigator.mediaSession.setActionHandler('seekforward', () => skip(10))
    navigator.mediaSession.setActionHandler('seekbackward', () => skip(-5))
  }, [state.articleId, state.title, state.feedTitle, toggle, skip])

  // Final flush on unmount.
  useEffect(() => () => { flush() }, [flush])

  const value = useMemo<PlayerContextValue>(() => ({
    ...state,
    audioRef,
    playArticle, toggle, seek, skip, setSpeed, close,
  }), [state, playArticle, toggle, seek, skip, setSpeed, close])

  return (
    <PlayerContext.Provider value={value}>
      <audio ref={audioRef} preload="metadata" style={{ display: 'none' }} />
      {children}
    </PlayerContext.Provider>
  )
}
