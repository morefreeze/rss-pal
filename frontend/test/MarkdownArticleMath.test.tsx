import { act, render } from '@testing-library/react'
import { afterEach, beforeEach, describe, it, expect, vi } from 'vitest'
import MarkdownArticle from '../src/components/MarkdownArticle'
import { escapeAmbiguousMathDollars } from '../src/util/mathShadow'

describe('math extraction rendering', () => {
 it('keeps digit-led equations and escapes prices', () => {
  expect(escapeAmbiguousMathDollars('$2x=0$')).toBe('$2x=0$')
  expect(escapeAmbiguousMathDollars('modulo $2$, degree $4$')).toBe('modulo $2$, degree $4$')
  expect(escapeAmbiguousMathDollars('Price $9 and $200')).toBe('Price \\$9 and \\$200')
 })
 it('does not pair a price with the next formula', () => {
  expect(escapeAmbiguousMathDollars('Cost $9; use $x \\in B$.')).toBe('Cost \\$9; use $x \\in B$.')
 })
 it('renders recovered inline and display formulas', () => {
  const { container } = render(<MarkdownArticle source={'Ring $x \\in B$, $2x=0$.\n\n$$\nx=\\frac{a}{b}\n$$'} />)
  expect(container.querySelectorAll('.katex')).toHaveLength(3)
  expect(container.querySelector('.katex-display')).not.toBeNull()
  expect(container.querySelector('.katex-error')).toBeNull()
 })
})

// Public equation from the thinking article that overflowed at 390px.
const WIDE_EQUATION = String.raw`\begin{aligned}
\mathbf{e} &= P(\mathbf{x}) & \text{embedding} \\
\mathbf{s}_0 &\sim \mathcal{N}(\mathbf{0}, \sigma^2 I_{n \cdot h}) \\
\mathbf{s}_i &= R(\mathbf{e}, \mathbf{s}_{i-1}) \quad\text{ for }i \in {1, \dots, r} & \small{\text{recurrent block; resembles a Transformer block}}\\
\mathbf{p} &= C(\mathbf{s}_r) & \text{unembedding}
\end{aligned}`

describe('display math scroll access', () => {
 let availableWidth: number
 let equationWidth: number
 let observers: MathResizeObserver[]
 const originalFonts = Object.getOwnPropertyDescriptor(document, 'fonts')

 class MathResizeObserver implements ResizeObserver {
  observe = vi.fn()
  unobserve = vi.fn()
  disconnect = vi.fn()
  constructor(private callback: ResizeObserverCallback) {
   observers.push(this)
  }
  resize() { this.callback([], this) }
 }

 beforeEach(() => {
  availableWidth = 340
  equationWidth = 936
  observers = []
  vi.stubGlobal('ResizeObserver', MathResizeObserver)
  // jsdom has no layout. Browser QA separately checks real KaTeX geometry;
  // these dimensions exercise the component's response to that geometry.
  vi.spyOn(Element.prototype, 'clientWidth', 'get').mockImplementation(function () {
   return this.classList.contains('katex-display') ? availableWidth : 0
  })
  vi.spyOn(Element.prototype, 'scrollWidth', 'get').mockImplementation(function () {
   if (!this.classList.contains('katex-display')) return 0
   return this.querySelector('annotation')?.textContent?.includes('recurrent block')
    ? Math.max(availableWidth, equationWidth) : availableWidth
  })
 })

 afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  if (originalFonts) Object.defineProperty(document, 'fonts', originalFonts)
  else Reflect.deleteProperty(document, 'fonts')
 })

 function renderEquation() {
  const rendered = render(<MarkdownArticle readOnly source={`Short $x^2$ and tall $\\dfrac{1}{\\sqrt{x^2+y^2}}$.\n\n$$\n${WIDE_EQUATION}\n$$\n\n$$\nx^2 + y^2 = z^2\n$$`} />)
  const [wide, short] = rendered.container.querySelectorAll<HTMLElement>('.katex-display')
  return { ...rendered, wide, short }
 }

 it('adds a named keyboard scroll region only for overflowing display math', () => {
  const { container, wide, short } = renderEquation()
  expect(wide.getAttribute('tabindex')).toBe('0')
  expect(wide.getAttribute('role')).toBe('region')
  expect(wide.getAttribute('aria-label')).toBe('数学公式，可横向滚动')
  expect(short.hasAttribute('tabindex')).toBe(false)
  expect(short.hasAttribute('role')).toBe(false)
  expect(container.querySelectorAll('.katex[tabindex]')).toHaveLength(0)
  expect(container.querySelectorAll('math')).toHaveLength(4)
  expect(container.querySelectorAll('annotation[encoding="application/x-tex"]')).toHaveLength(4)
  expect(wide.querySelector('annotation')?.textContent?.trim()).toBe(WIDE_EQUATION)
  expect(wide.querySelector('.katex-html')?.getAttribute('aria-hidden')).toBe('true')
  expect(container.querySelector('.katex-error')).toBeNull()
  wide.focus()
  expect(document.activeElement).toBe(wide)
 })

 it('updates keyboard access when the available reading width changes', () => {
  const { wide } = renderEquation()
  expect(wide.getAttribute('tabindex')).toBe('0')
  act(() => {
   availableWidth = 1000
   observers.forEach(observer => observer.resize())
  })
  expect(wide.hasAttribute('tabindex')).toBe(false)
  expect(wide.hasAttribute('role')).toBe(false)
  expect(wide.hasAttribute('aria-label')).toBe(false)
  act(() => {
   availableWidth = 340
   observers.forEach(observer => observer.resize())
  })
  expect(wide.getAttribute('tabindex')).toBe('0')
 })

 it('remeasures after math fonts load without a container resize', async () => {
  let finishLoading!: () => void
  Object.defineProperty(document, 'fonts', { configurable: true, value: {
   ready: new Promise<void>(resolve => { finishLoading = resolve }),
  } })
  equationWidth = 300
  const { wide, unmount } = renderEquation()
  expect(wide.hasAttribute('tabindex')).toBe(false)
  equationWidth = 936
  await act(async () => { finishLoading() })
  expect(wide.getAttribute('tabindex')).toBe('0')
  unmount()
  expect(observers.length).toBeGreaterThan(0)
  expect(observers.every(observer => observer.disconnect.mock.calls.length > 0)).toBe(true)
 })
})
