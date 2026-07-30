import { useEffect, useRef, useState } from 'react'
import type { MediaPlayerClass } from 'dashjs'

import { startYouTubePlayback } from '../api/client'
import Spinner from './Spinner'

interface Props {
  articleId: number
  originalURL: string
}

type Phase = 'loading' | 'ready' | 'error'

export default function YouTubeRelayPlayer({ articleId, originalURL }: Props) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [phase, setPhase] = useState<Phase>('loading')
  const [quality, setQuality] = useState(0)
  const [progressiveURL, setProgressiveURL] = useState('')
  const [compatibilityMode, setCompatibilityMode] = useState(false)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let cancelled = false
    let player: MediaPlayerClass | null = null
    let dashErrorHandler: ((event: unknown) => void) | null = null
    let dashErrorEvent = ''

    setPhase('loading')
    setQuality(0)
    setProgressiveURL('')
    setCompatibilityMode(false)

    const start = async () => {
      try {
        const playback = await startYouTubePlayback(articleId)
        if (cancelled) return

        const video = videoRef.current
        if (!video) throw new Error('video element is unavailable')
        setQuality(playback.quality)

        const canUseDASH = (
          playback.mode === 'dash' &&
          !!playback.manifest_url &&
          typeof window.MediaSource !== 'undefined'
        )
        if (!canUseDASH) {
          if (!playback.progressive_url) throw new Error('no compatible playback URL')
          setCompatibilityMode(true)
          setProgressiveURL(playback.progressive_url)
          setPhase('ready')
          return
        }

        const { MediaPlayer } = await import('dashjs')
        if (cancelled) return
        let fallbackUsed = false
        dashErrorHandler = () => {
          if (cancelled || fallbackUsed || !playback.progressive_url) {
            if (!cancelled && !playback.progressive_url) setPhase('error')
            return
          }
          fallbackUsed = true
          player?.destroy()
          player = null
          setCompatibilityMode(true)
          setProgressiveURL(playback.progressive_url)
          setPhase('ready')
        }
        player = MediaPlayer().create()
        dashErrorEvent = MediaPlayer.events.ERROR
        player.on(dashErrorEvent, dashErrorHandler)
        player.initialize(video, playback.manifest_url, false)
        setPhase('ready')
      } catch {
        if (!cancelled) setPhase('error')
      }
    }

    void start()
    return () => {
      cancelled = true
      if (player && dashErrorHandler && dashErrorEvent) {
        player.off(dashErrorEvent, dashErrorHandler)
      }
      player?.destroy()
    }
  }, [articleId, attempt])

  return (
    <div
      style={{
        width: '100%',
        maxWidth: 800,
        margin: '12px 0 20px',
      }}
    >
      <div
        style={{
          position: 'relative',
          width: '100%',
          aspectRatio: '16 / 9',
          background: '#000',
          borderRadius: 8,
          overflow: 'hidden',
        }}
      >
        <video
          ref={videoRef}
          src={progressiveURL || undefined}
          controls
          playsInline
          preload="metadata"
          aria-label="YouTube 视频播放器"
          style={{ width: '100%', height: '100%', display: 'block' }}
        />
        {phase !== 'ready' && (
          <div
            style={{
              position: 'absolute',
              inset: 0,
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              justifyContent: 'center',
              gap: 12,
              color: '#fff',
              background: 'rgba(0, 0, 0, 0.72)',
            }}
          >
            {phase === 'loading' ? (
              <>
                <Spinner size={28} color="#fff" />
                <span>正在准备 YouTube 视频…</span>
              </>
            ) : (
              <>
                <span>视频暂时无法加载</span>
                <button type="button" onClick={() => setAttempt(value => value + 1)}>
                  重试播放
                </button>
              </>
            )}
          </div>
        )}
      </div>
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          gap: 12,
          marginTop: 8,
          color: 'var(--fg-muted)',
          fontSize: 13,
        }}
      >
        <span>
          {compatibilityMode
            ? '兼容模式'
            : quality > 0
              ? `${quality}p · 北京中转`
              : '北京中转'}
        </span>
        <a href={originalURL} target="_blank" rel="noreferrer">
          在 YouTube 打开
        </a>
      </div>
    </div>
  )
}
