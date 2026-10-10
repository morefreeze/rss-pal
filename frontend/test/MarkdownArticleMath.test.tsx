import { render } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
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
