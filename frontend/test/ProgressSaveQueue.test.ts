import { afterEach, expect, it, vi } from 'vitest'
import { ProgressSaveQueue, progressAccountId } from '../src/utils/progressSaveQueue'
afterEach(() => { vi.useRealTimers(); localStorage.clear() })
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

const durable = (ownerId = 7) => ({ ownerId, storage: localStorage })
it('persists synchronously and restores after reload without a new scroll', async () => {
  const first = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  first.enqueue({ articleId: 4, scrollPosition: .8, isCompleted: false })
  expect(localStorage.length).toBe(1)
  const save = vi.fn().mockResolvedValue({})
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable()).flush()
  expect(save).toHaveBeenCalledWith({ articleId: 4, scrollPosition: .8, isCompleted: false })
  const afterAck = vi.fn()
  await new ProgressSaveQueue(afterAck, () => true, vi.fn(), durable()).flush()
  expect(afterAck).not.toHaveBeenCalled()
})
it('does not replay another account records', async () => {
  new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable()).enqueue({ articleId: 4, scrollPosition: .8, isCompleted: false })
  const save = vi.fn()
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable(8)).flush()
  expect(save).not.toHaveBeenCalled()
})
it('acknowledges only the durable snapshot that was sent', async () => {
  let finish!: (v: unknown) => void
  let authorized = true
  const old = new ProgressSaveQueue(() => new Promise(r => { finish = r }), () => authorized, vi.fn(), durable())
  old.enqueue({ articleId: 4, scrollPosition: .3, isCompleted: false })
  const flushing = old.flush()
  await vi.waitFor(() => expect(finish).toBeDefined())
  new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable()).enqueue({ articleId: 4, scrollPosition: .9, isCompleted: true })
  authorized = false
  finish({}); await flushing
  const save = vi.fn().mockResolvedValue({})
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable()).flush()
  expect(save).toHaveBeenCalledWith({ articleId: 4, scrollPosition: .9, isCompleted: true })
})
it('reset waits for another instance save and invalidates its old completion', async () => {
  let finish!: (v: unknown) => void
  const onSaved = vi.fn()
  const save = vi.fn(() => new Promise(r => { finish = r }))
  const old = new ProgressSaveQueue(save, () => true, onSaved, durable())
  old.enqueue({ articleId: 4, scrollPosition: 1, isCompleted: true })
  const flushing = old.flush()
  await vi.waitFor(() => expect(finish).toBeDefined())
  const second = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  const reset = vi.fn().mockResolvedValue(undefined)
  const replacing = second.replace(4, reset)
  expect(reset).not.toHaveBeenCalled()
  finish({}); await flushing; await replacing
  await old.flush()
  expect(save).toHaveBeenCalledTimes(1)
  expect(reset).toHaveBeenCalledTimes(1)
  expect(onSaved).not.toHaveBeenCalled()
})
it('keeps an in-memory fallback when storage is unavailable', async () => {
  const storage = { get length() { throw Error('denied') }, key() { throw Error('denied') }, getItem() { throw Error('denied') }, setItem() { throw Error('quota') }, removeItem() {} } as Storage
  const save = vi.fn().mockResolvedValue({})
  const queue = new ProgressSaveQueue(save, () => true, vi.fn(), { ownerId: 7, storage })
  queue.enqueue({ articleId: 4, scrollPosition: .6, isCompleted: false })
  await queue.flush()
  expect(save).toHaveBeenCalledTimes(1)
})
it('does not replace newer in-memory progress with stale storage after a quota failure', async () => {
  const first = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  first.enqueue({ articleId: 4, scrollPosition: .2, isCompleted: false })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw Error('quota') })
  const save = vi.fn().mockResolvedValue({})
  const queue = new ProgressSaveQueue(save, () => true, vi.fn(), durable())
  queue.enqueue({ articleId: 4, scrollPosition: .9, isCompleted: true })
  await queue.flush()
  expect(save).toHaveBeenCalledWith({ articleId: 4, scrollPosition: .9, isCompleted: true })
})
it('ignores malformed and invalid persisted progress while recovering valid rows', async () => {
  localStorage.setItem('rsspal:progress:v1:7:4:pending:broken', '{broken')
  localStorage.setItem('rsspal:progress:v1:7:5:pending:bad', JSON.stringify({ revision: 'bad', generation: '', entry: { articleId: 5, scrollPosition: 9, isCompleted: true } }))
  new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable()).enqueue({ articleId: 6, scrollPosition: .4, isCompleted: false })
  const save = vi.fn().mockResolvedValue({})
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable()).flush()
  expect(save).toHaveBeenCalledTimes(1)
  expect(save).toHaveBeenCalledWith({ articleId: 6, scrollPosition: .4, isCompleted: false })
})
it('requires a valid numeric authenticated account identity', () => {
  localStorage.setItem('token', 'present')
  for (const user of ['{broken', '{"id":"7"}', '{"id":-1}', 'null']) {
    localStorage.setItem('user', user)
    expect(progressAccountId()).toBeNull()
  }
  localStorage.setItem('user', '{"id":7}')
  expect(progressAccountId()).toBe(7)
  localStorage.removeItem('token')
  expect(progressAccountId()).toBeNull()
})

it('does not erase a concurrent tab enqueue between acknowledgement read and deletion', async () => {
  let finish!: (value: unknown) => void
  let authorized = true
  let inject = false
  const second = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  const storage = {
    get length() { return localStorage.length },
    key: (index: number) => localStorage.key(index),
    getItem: (key: string) => {
      const raw = localStorage.getItem(key)
      if (inject && raw?.includes('"scrollPosition":0.3')) {
        inject = false
        second.enqueue({ articleId: 4, scrollPosition: .9, isCompleted: true })
      }
      return raw
    },
    setItem: (key: string, value: string) => localStorage.setItem(key, value),
    removeItem: (key: string) => localStorage.removeItem(key),
  } as Storage
  const first = new ProgressSaveQueue(() => new Promise(r => { finish = r }), () => authorized, vi.fn(), { ownerId: 7, storage })
  first.enqueue({ articleId: 4, scrollPosition: .3, isCompleted: false })
  const flushing = first.flush()
  await vi.waitFor(() => expect(finish).toBeDefined())
  inject = true
  authorized = false
  finish({}); await flushing
  const save = vi.fn().mockResolvedValue({})
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable()).flush()
  expect(save).toHaveBeenCalledWith({ articleId: 4, scrollPosition: .9, isCompleted: true })
})

it('cancels reset when the account changes while waiting for an in-flight save', async () => {
  let finish!: (value: unknown) => void
  let authorized = true
  const queue = new ProgressSaveQueue(() => new Promise(r => { finish = r }), () => authorized, vi.fn(), durable())
  queue.enqueue({ articleId: 4, scrollPosition: 1, isCompleted: true })
  const flushing = queue.flush()
  await vi.waitFor(() => expect(finish).toBeDefined())
  const action = vi.fn().mockResolvedValue(undefined)
  const reset = queue.replace(4, action)
  const cancelled = expect(reset).rejects.toThrow('account')
  authorized = false
  finish({}); await flushing; await cancelled
  expect(action).not.toHaveBeenCalled()
})

it('cancels reset when the account changes while waiting for another instance lock', async () => {
  let finish!: (value: unknown) => void
  const first = new ProgressSaveQueue(() => new Promise(r => { finish = r }), () => true, vi.fn(), durable())
  first.enqueue({ articleId: 4, scrollPosition: 1, isCompleted: true })
  const flushing = first.flush()
  await vi.waitFor(() => expect(finish).toBeDefined())
  let authorized = true
  const action = vi.fn().mockResolvedValue(undefined)
  const second = new ProgressSaveQueue(vi.fn(), () => authorized, vi.fn(), durable())
  const reset = second.replace(4, action)
  const cancelled = expect(reset).rejects.toThrow('account')
  authorized = false
  finish({}); await flushing; await cancelled
  expect(action).not.toHaveBeenCalled()
})

it('suppresses the reset result if account changes during its request', async () => {
  let finish!: (value: unknown) => void
  let authorized = true
  const queue = new ProgressSaveQueue(vi.fn(), () => authorized, vi.fn(), durable())
  const resetting = queue.replace(4, () => new Promise(resolve => { finish = resolve }))
  const cancelled = expect(resetting).rejects.toThrow('account')
  await vi.waitFor(() => expect(finish).toBeDefined())
  authorized = false
  finish({ isCompleted: false })
  await cancelled
})

it('keeps higher concurrent enqueues when a lower snapshot writes later', async () => {
  const second = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  let inject = true
  const storage = {
    get length() { return localStorage.length },
    key: (index: number) => localStorage.key(index),
    getItem: (key: string) => localStorage.getItem(key),
    removeItem: (key: string) => localStorage.removeItem(key),
    setItem: (key: string, value: string) => {
      if (inject) { inject = false; second.enqueue({ articleId: 4, scrollPosition: .9, isCompleted: true }) }
      localStorage.setItem(key, value)
    },
  } as Storage
  new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), { ownerId: 7, storage })
    .enqueue({ articleId: 4, scrollPosition: .4, isCompleted: false })
  const save = vi.fn().mockResolvedValue({})
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable()).flush()
  expect(save).toHaveBeenCalledWith({ articleId: 4, scrollPosition: .9, isCompleted: true })
})

it('lets reset win when it occurs between enqueue merge and persistence', async () => {
  const old = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  old.enqueue({ articleId: 4, scrollPosition: .9, isCompleted: true })
  const second = new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), durable())
  let reset: Promise<unknown> | undefined
  let inject = true
  const storage = {
    get length() { return localStorage.length },
    key: (index: number) => localStorage.key(index),
    getItem: (key: string) => localStorage.getItem(key),
    removeItem: (key: string) => localStorage.removeItem(key),
    setItem: (key: string, value: string) => {
      if (inject) { inject = false; reset = second.replace(4, async () => {}) }
      localStorage.setItem(key, value)
    },
  } as Storage
  new ProgressSaveQueue(vi.fn(), () => true, vi.fn(), { ownerId: 7, storage })
    .enqueue({ articleId: 4, scrollPosition: .4, isCompleted: false })
  await reset
  const save = vi.fn().mockResolvedValue({})
  await new ProgressSaveQueue(save, () => true, vi.fn(), durable()).flush()
  expect(save).not.toHaveBeenCalled()
})

it('retains memory when persisted keys become unreadable', async () => {
  const save = vi.fn().mockResolvedValue({})
  const queue = new ProgressSaveQueue(save, () => true, vi.fn(), durable())
  queue.enqueue({ articleId: 4, scrollPosition: .7, isCompleted: false })
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw Error('denied') })
  await queue.flush()
  expect(save).toHaveBeenCalledWith({ articleId: 4, scrollPosition: .7, isCompleted: false })
})
