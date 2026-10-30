import { describe, expect, it } from 'vitest'
import { captureReadContinuation, findReadContinuation } from '../src/utils/readContinuation'

const rows = [{id:1}, {id:2, is_read:true}, {id:3}, {id:4}]

describe('read-above continuation', () => {
  it('records the next unread ID, excluding session reads', () => {
    const target = captureReadContinuation(rows, 1, false, new Set([3]))
    expect(target.nextUnreadId).toBe(4)
    expect(findReadContinuation([{id:1}, {id:99}, ...rows.slice(1)], target)).toBe(4)
  })
  it('skips a remembered article that becomes read before the refresh', () => {
    const target = captureReadContinuation(rows, 1, false)
    expect(findReadContinuation([{id:1}, {id:3,is_read:true}, {id:4}], target)).toBe(4)
  })
  it('waits for a known target and falls back after unread-only exhaustion', () => {
    const target = captureReadContinuation(rows, 1, true)
    expect(findReadContinuation([{id:4}], target)).toBeUndefined()
    expect(findReadContinuation([{id:4}], target, true)).toBe(4)
  })
  it('finds an unloaded continuation after the anchor, or at the unread-only start', () => {
    expect(findReadContinuation([{id:1}, {id:2}], captureReadContinuation([{id:1}], 1, false))).toBe(2)
    expect(findReadContinuation([{id:2}], captureReadContinuation([{id:1}], 1, true))).toBe(2)
  })
  it('does not choose an arbitrary row when the anchor or known target disappears from an all-articles list', () => {
    expect(findReadContinuation([{id:99}], captureReadContinuation(rows, 1, false), true)).toBeUndefined()
    expect(findReadContinuation([{id:99}], captureReadContinuation([{id:1}], 1, false), true)).toBeUndefined()
  })
})
