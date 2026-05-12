import { useEffect, useState, useCallback, useRef } from 'react'

// useReadingChrome owns the "chrome visible" state in reading mode.
// Adds `reading-mode-active` to body on enable; toggles
// `reading-chrome-visible` based on user input.
//
// Reveal triggers: tap article center (callback exposed to caller),
// scroll up ≥ THRESHOLD, page near top.
// Hide triggers: scroll down ≥ THRESHOLD.
//
// `enabled=false` removes both classes and unbinds listeners.
const THRESHOLD = 30
const TOP_REVEAL = 40

export function useReadingChrome(enabled: boolean): {
  chromeVisible: boolean
  toggle: () => void
} {
  const [chromeVisible, setChromeVisible] = useState(false)
  const lastY = useRef(0)
  const accum = useRef(0)

  useEffect(() => {
    if (!enabled) {
      document.body.classList.remove('reading-mode-active')
      document.body.classList.remove('reading-chrome-visible')
      return
    }
    document.body.classList.add('reading-mode-active')
    setChromeVisible(false)
    lastY.current = window.scrollY
    accum.current = 0

    const onScroll = () => {
      const y = window.scrollY
      const dy = y - lastY.current
      lastY.current = y
      if (y < TOP_REVEAL) {
        setChromeVisible(true)
        accum.current = 0
        return
      }
      // Accumulate same-direction delta; reset on direction change
      if ((accum.current > 0 && dy < 0) || (accum.current < 0 && dy > 0)) {
        accum.current = dy
      } else {
        accum.current += dy
      }
      if (accum.current >= THRESHOLD) {
        setChromeVisible(false)
        accum.current = 0
      } else if (accum.current <= -THRESHOLD) {
        setChromeVisible(true)
        accum.current = 0
      }
    }

    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      document.body.classList.remove('reading-mode-active')
      document.body.classList.remove('reading-chrome-visible')
    }
  }, [enabled])

  useEffect(() => {
    if (!enabled) return
    document.body.classList.toggle('reading-chrome-visible', chromeVisible)
  }, [chromeVisible, enabled])

  const toggle = useCallback(() => {
    setChromeVisible(v => !v)
  }, [])

  return { chromeVisible, toggle }
}
