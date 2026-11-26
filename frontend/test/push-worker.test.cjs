const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const { test } = require('node:test')

function worker() {
  const handlers = new Map()
  const badges = []
  const shown = []
  const opened = []
  const self = {
    addEventListener: (type, handler) => handlers.set(type, handler),
    navigator: { setAppBadge: async (count) => badges.push(count) },
    registration: { showNotification: async (title, options) => shown.push({ title, options }) },
    location: { origin: 'https://rss.example' },
    clients: { matchAll: async () => [], openWindow: async (href) => opened.push(href) },
  }
  const script = fs.readFileSync(path.join(__dirname, '../public/push-worker.js'), 'utf8')
  vm.runInNewContext(script, { self, URL })
  return { handlers, badges, shown, opened }
}

test('an unread digest produces one visible notification with the total badge', async () => {
  const { handlers, badges, shown } = worker()
  let work
  handlers.get('push')({
    data: { json: () => ({ app_badge: 20, notification: { body: '新增 2 篇，共 20 篇未读' } }) },
    waitUntil: (promise) => { work = promise },
  })
  await work
  assert.deepEqual(badges, [20])
  assert.equal(shown.length, 1)
  assert.equal(shown[0].title, 'RSS Pal')
  assert.equal(shown[0].options.body, '新增 2 篇，共 20 篇未读')
})

test('clicking a digest opens the article list on the same site', async () => {
  const { handlers, opened } = worker()
  let work
  let closed = false
  handlers.get('notificationclick')({
    notification: { close: () => { closed = true } },
    waitUntil: (promise) => { work = promise },
  })
  await work
  assert.equal(closed, true)
  assert.deepEqual(opened, ['https://rss.example/articles'])
})
