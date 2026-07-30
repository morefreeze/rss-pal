import { useEffect, useRef, useState } from 'react'
import type { MediaPlayerClass } from 'dashjs'

import {
  detectYouTubeBridge,
  resolveYouTubePlayback,
  YouTubeBridgeError,
  type BrowserPlayback,
  type ProgressiveTrack,
} from '../youtube/bridge'
import { buildYouTubeMpd } from '../youtube/mpd'
import Spinner from './Spinner'

interface Props {
  videoId: string
  start?: number
  originalURL: string
}

type Phase =
  | 'checking'
  | 'idle'
  | 'resolving'
  | 'ready'
  | 'unavailable'
  | 'outdated'
  | 'error'

class MediaAttachmentError extends Error {}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

export default function YouTubeBrowserPlayer({
  videoId,
  start,
  originalURL,
}: Props) {
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const lastVideoRef = useRef<HTMLVideoElement | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const dashRef = useRef<MediaPlayerClass | null>(null)
  const dashErrorRef = useRef<((event?: unknown) => void) | null>(null)
  const dashErrorEventRef = useRef('')
  const mpdURLRef = useRef('')
  const autoRetryUsedRef = useRef(false)
  const mountedRef = useRef(true)
  const operationRef = useRef(0)
  const resolvingRef = useRef(false)
  const playbackRef = useRef<BrowserPlayback | null>(null)
  const compatibilityModeRef = useRef(false)
  const phaseRef = useRef<Phase>('checking')
  const startAppliedRef = useRef(false)

  const [phase, setPhase] = useState<Phase>('checking')
  const [quality, setQuality] = useState(0)
  const [progressiveURL, setProgressiveURL] = useState('')
  const [compatibilityMode, setCompatibilityMode] = useState(false)
  const [errorMessage, setErrorMessage] = useState('视频暂时无法加载')

  function updatePhase(nextPhase: Phase) {
    phaseRef.current = nextPhase
    if (mountedRef.current) setPhase(nextPhase)
  }

  function clearVideoElement() {
    const video = videoRef.current ?? lastVideoRef.current
    if (!video) return
    video.removeAttribute('src')
    try {
      video.load()
    } catch {
      // A detached media element may reject load during teardown.
    }
  }

  function detachCurrent(
    expectedPlayer?: MediaPlayerClass,
    expectedManifestURL?: string,
  ): boolean {
    if (
      expectedPlayer !== undefined &&
      (
        dashRef.current !== expectedPlayer ||
        mpdURLRef.current !== expectedManifestURL
      )
    ) {
      return false
    }

    const player = dashRef.current
    const handler = dashErrorRef.current
    const event = dashErrorEventRef.current
    const manifestURL = mpdURLRef.current

    dashRef.current = null
    dashErrorRef.current = null
    dashErrorEventRef.current = ''
    mpdURLRef.current = ''

    if (player && handler && event) {
      try {
        player.off(event, handler)
      } catch {
        // Teardown must continue even if dash.js has already detached.
      }
    }
    if (player) {
      try {
        player.destroy()
      } catch {
        // Teardown must continue even if dash.js has already failed.
      }
    }
    if (manifestURL) {
      URL.revokeObjectURL(manifestURL)
    }
    clearVideoElement()
    return true
  }

  function clearPlayback() {
    const controller = abortRef.current
    abortRef.current = null
    controller?.abort()
    detachCurrent()
    playbackRef.current = null
    compatibilityModeRef.current = false
  }

  function isCurrentOperation(operation: number): boolean {
    return (
      mountedRef.current &&
      operationRef.current === operation
    )
  }

  function showProgressive(
    playback: BrowserPlayback,
    track: ProgressiveTrack,
    operation: number,
  ) {
    if (!isCurrentOperation(operation)) return
    playbackRef.current = playback
    compatibilityModeRef.current = true
    setProgressiveURL(track.url)
    setQuality(track.height)
    setCompatibilityMode(true)
    updatePhase('ready')
  }

  function showError(error: unknown) {
    if (
      error instanceof YouTubeBridgeError &&
      error.code === 'EXTENSION_UNAVAILABLE'
    ) {
      updatePhase('unavailable')
      return
    }

    if (
      error instanceof YouTubeBridgeError &&
      error.code === 'LOGIN_REQUIRED'
    ) {
      setErrorMessage('请先在 Chrome 中登录 YouTube')
    } else if (
      error instanceof YouTubeBridgeError &&
      error.code === 'LOCAL_NETWORK_ERROR'
    ) {
      setErrorMessage('本机无法连接 YouTube，请检查 Clash')
    } else {
      setErrorMessage('视频暂时无法加载')
    }
    updatePhase('error')
  }

  function handleMediaFailure(operation: number) {
    if (
      !isCurrentOperation(operation) ||
      phaseRef.current !== 'ready'
    ) {
      return
    }

    if (autoRetryUsedRef.current) {
      detachCurrent()
      playbackRef.current = null
      setProgressiveURL('')
      setQuality(0)
      setCompatibilityMode(false)
      setErrorMessage('视频暂时无法加载')
      updatePhase('error')
      return
    }

    autoRetryUsedRef.current = true
    void resolveAndAttach()
  }

  function useProgressiveFallback(
    playback: BrowserPlayback,
    operation: number,
    expectedPlayer?: MediaPlayerClass,
    expectedManifestURL?: string,
  ) {
    if (!playback.progressive || !isCurrentOperation(operation)) return false
    if (expectedPlayer !== undefined) {
      if (!detachCurrent(expectedPlayer, expectedManifestURL)) return false
    } else {
      detachCurrent()
    }
    showProgressive(playback, playback.progressive, operation)
    return true
  }

  async function attach(playback: BrowserPlayback, operation: number) {
    if (!isCurrentOperation(operation)) return

    const video = videoRef.current
    if (!video) throw new MediaAttachmentError()
    playbackRef.current = playback

    const canUseDASH = (
      playback.mode === 'dash' &&
      !!playback.video &&
      !!playback.audio &&
      typeof window.MediaSource !== 'undefined'
    )

    if (!canUseDASH) {
      if (!playback.progressive) throw new MediaAttachmentError()
      showProgressive(playback, playback.progressive, operation)
      return
    }

    let manifestURL = ''
    let player: MediaPlayerClass | null = null
    let errorHandler: ((event?: unknown) => void) | null = null
    let errorEvent = ''

    try {
      const manifest = buildYouTubeMpd(playback)
      manifestURL = URL.createObjectURL(
        new Blob([manifest], { type: 'application/dash+xml' }),
      )
      const { MediaPlayer } = await import('dashjs')

      if (!isCurrentOperation(operation)) {
        URL.revokeObjectURL(manifestURL)
        return
      }

      player = MediaPlayer().create()
      errorEvent = MediaPlayer.events.ERROR
      const ownedPlayer = player
      const ownedManifestURL = manifestURL
      errorHandler = () => {
        if (
          !isCurrentOperation(operation) ||
          dashRef.current !== ownedPlayer ||
          mpdURLRef.current !== ownedManifestURL
        ) {
          return
        }
        if (
          useProgressiveFallback(
            playback,
            operation,
            ownedPlayer,
            ownedManifestURL,
          )
        ) {
          return
        }
        handleMediaFailure(operation)
      }

      dashRef.current = player
      dashErrorRef.current = errorHandler
      dashErrorEventRef.current = errorEvent
      mpdURLRef.current = manifestURL
      player.on(errorEvent, errorHandler)
      player.initialize(video, manifestURL, false)

      if (
        !isCurrentOperation(operation) ||
        dashRef.current !== player ||
        mpdURLRef.current !== manifestURL
      ) {
        return
      }

      compatibilityModeRef.current = false
      setProgressiveURL('')
      setQuality(playback.quality)
      setCompatibilityMode(false)
      updatePhase('ready')
    } catch {
      const isOwned = (
        player !== null &&
        dashRef.current === player &&
        mpdURLRef.current === manifestURL
      )

      if (isOwned && player) {
        detachCurrent(player, manifestURL)
      } else {
        if (player && errorHandler && errorEvent) {
          try {
            player.off(errorEvent, errorHandler)
          } catch {
            // Best-effort cleanup for a partially initialized player.
          }
        }
        if (player) {
          try {
            player.destroy()
          } catch {
            // Best-effort cleanup for a partially initialized player.
          }
        }
        if (manifestURL) URL.revokeObjectURL(manifestURL)
      }

      if (!isCurrentOperation(operation)) return
      if (playback.progressive) {
        showProgressive(playback, playback.progressive, operation)
        return
      }
      throw new MediaAttachmentError()
    }
  }

  async function resolveAndAttach() {
    if (!mountedRef.current || resolvingRef.current) return

    resolvingRef.current = true
    const operation = operationRef.current + 1
    operationRef.current = operation

    const previousController = abortRef.current
    abortRef.current = null
    previousController?.abort()
    detachCurrent()
    playbackRef.current = null
    compatibilityModeRef.current = false
    startAppliedRef.current = false
    setProgressiveURL('')
    setQuality(0)
    setCompatibilityMode(false)
    setErrorMessage('视频暂时无法加载')
    updatePhase('resolving')

    const controller = new AbortController()
    abortRef.current = controller
    let retryAttachment = false

    try {
      const playback = await resolveYouTubePlayback(videoId, controller.signal)
      if (!isCurrentOperation(operation) || controller.signal.aborted) return

      const expiresAt = Date.parse(playback.expiresAt)
      if (
        !Number.isFinite(expiresAt) ||
        expiresAt - Date.now() < 30_000
      ) {
        throw new YouTubeBridgeError('PLAYBACK_EXPIRED')
      }

      await attach(playback, operation)
    } catch (error) {
      if (
        !isCurrentOperation(operation) ||
        controller.signal.aborted ||
        isAbortError(error)
      ) {
        return
      }

      if (error instanceof MediaAttachmentError) {
        if (!autoRetryUsedRef.current) {
          autoRetryUsedRef.current = true
          retryAttachment = true
        } else {
          showError(error)
        }
      } else {
        showError(error)
      }
    } finally {
      if (abortRef.current === controller) abortRef.current = null
      if (operationRef.current === operation) resolvingRef.current = false
      if (retryAttachment && isCurrentOperation(operation)) {
        void resolveAndAttach()
      }
    }
  }

  function handleNativeError() {
    if (phaseRef.current !== 'ready') return
    const operation = operationRef.current
    const playback = playbackRef.current
    if (!playback) return

    if (
      !compatibilityModeRef.current &&
      playback.progressive &&
      useProgressiveFallback(playback, operation)
    ) {
      return
    }
    handleMediaFailure(operation)
  }

  function handleLoadedMetadata() {
    if (startAppliedRef.current || !start || !Number.isFinite(start) || start <= 0) {
      return
    }
    const video = videoRef.current
    if (
      !video ||
      !Number.isFinite(video.duration) ||
      video.duration <= 0 ||
      start >= video.duration
    ) {
      return
    }
    video.currentTime = start
    startAppliedRef.current = true
  }

  function handleVisibleRetry() {
    autoRetryUsedRef.current = false
    void resolveAndAttach()
  }

  useEffect(() => {
    mountedRef.current = true
    void detectYouTubeBridge()
      .then(result => {
        if (!mountedRef.current) return
        if (!result.available) {
          updatePhase('unavailable')
        } else if (!result.compatible) {
          updatePhase('outdated')
        } else {
          updatePhase('idle')
        }
      })
      .catch(() => {
        if (mountedRef.current) updatePhase('unavailable')
      })

    return () => {
      mountedRef.current = false
      operationRef.current += 1
      resolvingRef.current = false
      clearPlayback()
    }
  }, [])

  const showVideo = (
    phase === 'resolving' ||
    phase === 'ready' ||
    phase === 'error'
  )

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
        {showVideo && (
          <video
            ref={element => {
              videoRef.current = element
              if (element) lastVideoRef.current = element
            }}
            src={progressiveURL || undefined}
            controls
            playsInline
            preload="metadata"
            aria-label="YouTube 视频播放器"
            onLoadedMetadata={handleLoadedMetadata}
            onError={handleNativeError}
            style={{ width: '100%', height: '100%', display: 'block' }}
          />
        )}
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
            {(phase === 'checking' || phase === 'resolving') && (
              <Spinner size={28} color="#fff" />
            )}
            {phase === 'checking' && <span>正在检查 RSS Pal 扩展…</span>}
            {phase === 'idle' && (
              <button
                type="button"
                onClick={() => {
                  autoRetryUsedRef.current = false
                  void resolveAndAttach()
                }}
              >
                使用已登录的 Chrome 播放
              </button>
            )}
            {phase === 'resolving' && (
              <span>正在通过已登录的 YouTube 准备视频…</span>
            )}
            {phase === 'unavailable' && (
              <span>需要安装并启用 RSS Pal Chrome 扩展</span>
            )}
            {phase === 'outdated' && <span>请重新加载 RSS Pal 扩展</span>}
            {phase === 'error' && (
              <>
                <span>{errorMessage}</span>
                <button type="button" onClick={handleVisibleRetry}>
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
          {phase === 'ready' && quality > 0
            ? compatibilityMode
              ? `${quality}p · 本机 Chrome · 兼容模式`
              : `${quality}p · 本机 Chrome`
            : ''}
        </span>
        <a
          href={originalURL}
          target="_blank"
          rel="noopener noreferrer"
        >
          在 YouTube 打开
        </a>
      </div>
    </div>
  )
}
