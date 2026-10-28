import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import MarkdownArticle from '../src/components/MarkdownArticle'
import { escapeAmbiguousMathDollars } from '../src/util/mathShadow'

describe('numeric symbolic math dollars', () => {
  it('keeps numeric products and the following TeX formula intact', () => {
    const source = 'Training takes $6N$, $2ND$, $5x+3$ and $C \\approx 6ND$ operations.'
    expect(escapeAmbiguousMathDollars(source)).toBe(source)
    const { container } = render(<MarkdownArticle source={source} />)
    expect(container.querySelectorAll('.katex')).toHaveLength(4)
    expect(container.querySelector('.katex-error')).toBeNull()
  })

  it('keeps prices escaped and does not steal a following formula opener', () => {
    expect(escapeAmbiguousMathDollars('Price $9 dollars$ and $C \\approx 6ND$.'))
      .toBe('Price \\$9 dollars\\$ and $C \\approx 6ND$.')
    expect(escapeAmbiguousMathDollars('Price $9.99 and $200; use $6N$.'))
      .toBe('Price \\$9.99 and \\$200; use $6N$.')
    expect(escapeAmbiguousMathDollars('Cost $9; use $6N$ and $C \\approx 6ND$.'))
      .toBe('Cost \\$9; use $6N$ and $C \\approx 6ND$.')
  })

  it('keeps code literal and leaves unmatched or escaped dollars alone', () => {
    const source = '`$6N$ and $C \\approx 6ND$`\n\n```text\n$2ND$\n```\n\nUnmatched $6N and \\$9.'
    expect(escapeAmbiguousMathDollars(source)).toBe(source)
    const { container } = render(<MarkdownArticle source={source} />)
    expect(container.querySelectorAll('.katex')).toHaveLength(0)
    expect(container.querySelector('code')?.textContent).toBe('$6N$ and $C \\approx 6ND$')
  })
})
