// Mirror of backend/internal/rss/content.go::stripJinaMathShadow.
// Drops the Unicode "shadow" Jina Reader appends after each $math$ span.
// Kept here for client-side cleanup of articles already stored in the DB
// before the server-side strip was added. See:
//   docs/superpowers/specs/2026-05-07-math-formula-rendering-design.md

const SIGNAL_RANGES: ReadonlyArray<readonly [number, number]> = [
  [0x200b, 0x200b],
  [0x2212, 0x2212],
  [0x00b0, 0x00ff],
  [0x2200, 0x23ff],
  [0x2a00, 0x2aff],
  [0x2070, 0x209f],
  [0x0391, 0x03c9],
]

function isSignalCp(cp: number): boolean {
  for (const [lo, hi] of SIGNAL_RANGES) {
    if (cp >= lo && cp <= hi) return true
  }
  return false
}

function isAsciiLetterCp(cp: number): boolean {
  return (cp >= 0x41 && cp <= 0x5a) || (cp >= 0x61 && cp <= 0x7a)
}

function isAsciiDigitCp(cp: number): boolean {
  return cp >= 0x30 && cp <= 0x39
}

function mathBodyQualifies(body: string): boolean {
  if (body.length === 0) return false
  const first = body.codePointAt(0)
  if (first === undefined) return false
  if (isAsciiDigitCp(first)) return false
  return true
}

function isSentencePunctCp(cp: number): boolean {
  return cp === 0x2c /* , */ || cp === 0x2e /* . */ ||
    cp === 0x3b /* ; */ || cp === 0x3a /* : */ ||
    cp === 0x21 /* ! */ || cp === 0x3f /* ? */
}

// escapeAmbiguousMathDollars mirrors backend/internal/rss/content.go.
// Escapes pairs of literal `$` whose body looks like prose (digit-led, no
// LaTeX specials) so remark-math doesn't pair them into a math span. Keeps
// real math (`$\sqrt{x}$`, `$x = 1$`) untouched. Idempotent on already-
// escaped input. Applied client-side so articles stored before the
// server-side escape is deployed render correctly.
export function escapeAmbiguousMathDollars(md: string): string {
  const r = Array.from(md)
  let out = ''
  let i = 0
  while (i < r.length) {
    const c = r[i]
    if (c === '\\' && i + 1 < r.length) {
      out += c + r[i + 1]
      i += 2
      continue
    }
    if (c !== '$') {
      out += c
      i++
      continue
    }
    let j = i + 1
    while (j < r.length && r[j] !== '\n') {
      if (r[j] === '\\' && j + 1 < r.length) {
        j += 2
        continue
      }
      if (r[j] === '$') break
      j++
    }
    if (j >= r.length || r[j] !== '$') {
      out += c
      i++
      continue
    }
    const body = r.slice(i + 1, j).join('')
    if (shouldEscapeProseDollarPair(body)) {
      out += '\\$' + body + '\\$'
      i = j + 1
      continue
    }
    out += r.slice(i, j + 1).join('')
    i = j + 1
  }
  return out
}

function shouldEscapeProseDollarPair(body: string): boolean {
  if (body.length === 0) return false
  const first = body.codePointAt(0)
  if (first === undefined) return false
  if (!isAsciiDigitCp(first)) return false
  for (const ch of body) {
    if (ch === '\\' || ch === '{' || ch === '}' || ch === '_' || ch === '^') {
      return false
    }
  }
  return true
}

export function stripMathShadow(md: string): string {
  const r = Array.from(md)
  let out = ''
  let i = 0
  while (i < r.length) {
    if (r[i] !== '$') {
      out += r[i]
      i++
      continue
    }
    let j = i + 1
    while (j < r.length && r[j] !== '$' && r[j] !== '\n') j++
    if (j >= r.length || r[j] === '\n') {
      out += r[i]
      i++
      continue
    }
    const body = r.slice(i + 1, j).join('')
    if (!mathBodyQualifies(body)) {
      out += r[i]
      i++
      continue
    }
    out += r.slice(i, j + 1).join('')
    i = j + 1
    let end = i
    let hasSignal = false
    while (end < r.length) {
      const c = r[end]
      if (c === '\n') break
      const cp = c.codePointAt(0)!
      if (isSentencePunctCp(cp)) {
        const next = end + 1
        if (next >= r.length) break
        const nc = r[next]
        if (nc === ' ' || nc === '\t' || nc === '\n' || nc === '$') break
        // Otherwise (digit/letter follows), continue consuming the punct.
      }
      if (isAsciiLetterCp(cp)) {
        let k = end
        while (k < r.length && isAsciiLetterCp(r[k].codePointAt(0)!)) k++
        if (k - end >= 3) break
        end = k
        continue
      }
      if (isSignalCp(cp)) hasSignal = true
      end++
    }
    while (end > i && (r[end - 1] === ' ' || r[end - 1] === '\t')) end--
    if (hasSignal) i = end
  }
  return out
}
