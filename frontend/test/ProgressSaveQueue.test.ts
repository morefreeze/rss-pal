import { afterEach, expect, it, vi } from 'vitest'
import { ProgressSaveQueue } from '../src/utils/progressSaveQueue'
afterEach(() => vi.useRealTimers())
it('retries an acknowledged high-water mark without requiring another scroll', async () => {
  vi.useFakeTimers()
  const save = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ saved: true })
  const queue = new ProgressSaveQueue(save, () => true, vi.fn())
  queue.enqueue({ articleId: 4, scrollPosition: .8, isCompleted: false })
  await queue.flush()
  await vi.advanceTimersByTimeAsync(1000)
  expect(save).toHaveBeenCalledTimes(2)
  expect(save.mock.calls[1][0].articleId).toBe(4)
})
it('preserves a newer pending scroll while an older request is in flight', async () => {
  let finish!: (v: unknown) => void
  const save = vi.fn().mockImplementationOnce(() => new Promise(r => { finish = r })).mockResolvedValue({})
  const queue = new ProgressSaveQueue(save, () => true, vi.fn())
  queue.enqueue({ articleId: 4, scrollPosition: .3, isCompleted: false })
  const flushing = queue.flush()
  queue.enqueue({ articleId: 4, scrollPosition: .9, isCompleted: true })
  finish({}); await flushing
  expect(save.mock.calls[1][0]).toEqual({ articleId: 4, scrollPosition: .9, isCompleted: true })
})
it('does not send pending progress under a different login', async () => {
  const save = vi.fn()
  const queue = new ProgressSaveQueue(save, () => false, vi.fn())
  queue.enqueue({ articleId: 4, scrollPosition: .8, isCompleted: false })
  await queue.flush()
  expect(save).not.toHaveBeenCalled()
})
it('drops failed completion retries before an explicit mark unread', async () => {
  vi.useFakeTimers()
  const save = vi.fn().mockRejectedValue(new Error('offline'))
  const queue = new ProgressSaveQueue(save, () => true, vi.fn())
  queue.enqueue({ articleId: 4, scrollPosition: 1, isCompleted: true })
  await queue.flush()
  const reset = vi.fn().mockResolvedValue(undefined)
  await queue.replace(4, reset)
  await vi.advanceTimersByTimeAsync(30000)
  expect(save).toHaveBeenCalledTimes(1)
  expect(reset).toHaveBeenCalledTimes(1)
})
it('orders explicit reset after an in-flight save and suppresses its old UI callback', async () => {
  let finish!: (v: unknown) => void
  const onSaved = vi.fn()
  const queue = new ProgressSaveQueue(() => new Promise(r => { finish = r }), () => true, onSaved)
  queue.enqueue({ articleId: 4, scrollPosition: 1, isCompleted: true })
  const flushing = queue.flush()
  const reset = vi.fn().mockResolvedValue(undefined)
  const replacing = queue.replace(4, reset)
  expect(reset).not.toHaveBeenCalled()
  finish({}); await flushing; await replacing
  expect(reset).toHaveBeenCalledTimes(1)
  expect(onSaved).not.toHaveBeenCalled()
})
