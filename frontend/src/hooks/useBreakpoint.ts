import { useEffect, useState } from 'react'

export type Breakpoint = 'phone' | 'tablet' | 'desktop'

// Phone ≤ 640px, Tablet 641–1024px, Desktop > 1024px. SSR-safe default:
// if `window` is missing we return 'desktop' to mirror legacy markup.
function compute(): Breakpoint {
  if (typeof window === 'undefined') return 'desktop'
  if (window.matchMedia('(max-width: 640px)').matches) return 'phone'
  if (window.matchMedia('(max-width: 1024px)').matches) return 'tablet'
  return 'desktop'
}

export function useBreakpoint(): Breakpoint {
  const [bp, setBp] = useState<Breakpoint>(() => compute())

  useEffect(() => {
    const phone = window.matchMedia('(max-width: 640px)')
    const tablet = window.matchMedia('(max-width: 1024px)')
    const update = () => setBp(compute())
    phone.addEventListener('change', update)
    tablet.addEventListener('change', update)
    return () => {
      phone.removeEventListener('change', update)
      tablet.removeEventListener('change', update)
    }
  }, [])

  return bp
}
