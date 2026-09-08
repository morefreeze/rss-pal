import { StrictMode } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import FeedListPage from '../src/pages/FeedListPage'
import LoginPage from '../src/pages/LoginPage'
import RegisterPage from '../src/pages/RegisterPage'
import { authSearch, parseAuthIntent, postAuthURL } from '../src/utils/authIntent'

const apiMocks = vi.hoisted(() => ({
  login: vi.fn(),
  register: vi.fn(),
  api: { post: vi.fn() },
  getFeeds: vi.fn(),
  addFeed: vi.fn(),
  deleteFeed: vi.fn(),
  fetchFeedNow: vi.fn(),
  previewFeed: vi.fn(),
  toggleFeedActive: vi.fn(),
  exportOPML: vi.fn(),
  createOneoffLinkSet: vi.fn(),
  capturePDFURL: vi.fn(),
  getMyBookmarkletToken: vi.fn(),
}))

vi.mock('../src/api/client', () => apiMocks)

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{location.pathname}{location.search}</output>
}

function renderAuth(path: string) {
  const onLogin = vi.fn()
  render(
    <MemoryRouter initialEntries={[path]}>
      <LocationProbe />
      <Routes>
        <Route path="/login" element={<LoginPage onLogin={onLogin} />} />
        <Route path="/register" element={<RegisterPage onLogin={onLogin} />} />
        <Route path="/articles" element={<div>articles destination</div>} />
        <Route path="/feeds" element={<div>feeds destination</div>} />
      </Routes>
    </MemoryRouter>,
  )
  return { onLogin }
}

function submitLogin() {
  fireEvent.change(screen.getByPlaceholderText('用户名'), { target: { value: 'reader' } })
  fireEvent.change(screen.getByPlaceholderText('密码'), { target: { value: 'secret-password' } })
  fireEvent.click(screen.getByRole('button', { name: '登录' }))
}

function submitRegistration() {
  fireEvent.change(screen.getByPlaceholderText('邀请码'), { target: { value: 'invite-42' } })
  fireEvent.change(screen.getByPlaceholderText('用户名'), { target: { value: 'new-reader' } })
  fireEvent.change(screen.getByPlaceholderText('密码（至少 6 位）'), { target: { value: 'secret-password' } })
  fireEvent.click(screen.getByRole('button', { name: '注册' }))
}

function renderFeeds(path: string, strict = false) {
  const tree = (
    <MemoryRouter initialEntries={[path]}>
      <LocationProbe />
      <Routes>
        <Route path="/feeds" element={<FeedListPage />} />
      </Routes>
    </MemoryRouter>
  )
  return render(strict ? <StrictMode>{tree}</StrictMode> : tree)
}

beforeEach(() => {
  vi.clearAllMocks()
  apiMocks.api.post.mockRejectedValue(new Error('already initialized'))
  apiMocks.login.mockResolvedValue({ user: { id: 1, username: 'reader' } })
  apiMocks.register.mockResolvedValue({ user: { id: 2, username: 'new-reader' } })
  apiMocks.getFeeds.mockResolvedValue([])
  apiMocks.previewFeed.mockResolvedValue({
    feed_title: 'Preview feed',
    feed_type: 'rss',
    actual_url: 'https://source.example/feed',
    items: [],
  })
})

describe('authentication intents', () => {
  it.each([
    '/login?intent=use',
    '/login',
    '/login?intent=unknown&next=https://evil.example',
    '/login?intent=subscribe&source=javascript%3Aalert(1)',
  ])('sends use, default, and invalid login intents to articles: %s', async path => {
    renderAuth(path)
    submitLogin()
    await screen.findByText('articles destination')
    expect(screen.getByTestId('location').textContent).toBe('/articles')
    expect(apiMocks.login).toHaveBeenCalledWith('reader', 'secret-password', true)
  })

  it('preserves a canonical subscribe intent through login, registration, and both auth links', async () => {
    const source = 'https://source.example/文章?q=阅读&x=1'
    const search = `?intent=subscribe&source=${encodeURIComponent(source)}`
    const { onLogin } = renderAuth(`/login${search}`)

    const registerLink = screen.getByRole('link', { name: '使用邀请码注册' })
    expect(registerLink.getAttribute('href')).toBe(`/register${search}`)
    fireEvent.click(registerLink)
    expect(screen.getByTestId('location').textContent).toBe(`/register${search}`)
    expect(screen.getByPlaceholderText('邀请码')).toBeTruthy()
    expect(screen.getByRole('link', { name: '已有账号？登录' }).getAttribute('href')).toBe(`/login${search}`)

    submitRegistration()
    await screen.findByText('feeds destination')
    expect(screen.getByTestId('location').textContent).toBe(`/feeds?add=1&source=${encodeURIComponent(source)}`)
    expect(apiMocks.register).toHaveBeenCalledWith('new-reader', 'secret-password', 'invite-42')
    expect(onLogin).toHaveBeenCalledWith({ id: 2, username: 'new-reader' })
  })

  it('sends a successful subscribe login to the feed preview URL without double encoding', async () => {
    const source = 'https://source.example/文章?q=阅读'
    renderAuth(`/login?source=${encodeURIComponent(source)}&intent=subscribe`)
    submitLogin()
    await screen.findByText('feeds destination')
    expect(screen.getByTestId('location').textContent).toBe(`/feeds?add=1&source=${encodeURIComponent(source)}`)
    expect(screen.getByTestId('location').textContent).not.toContain('%25E6')
  })

  it('keeps a failed login on the canonical intent URL without exposing the password', async () => {
    apiMocks.login.mockRejectedValue(new Error('request included secret-password'))
    const source = 'https://source.example/feed'
    renderAuth(`/login?source=${encodeURIComponent(source)}&intent=subscribe`)
    submitLogin()
    expect(await screen.findByText('用户名或密码错误')).toBeTruthy()
    expect(screen.getByTestId('location').textContent).toBe(`/login?source=${encodeURIComponent(source)}&intent=subscribe`)
    expect(document.body.textContent).not.toContain('secret-password')
  })

  it('keeps a failed registration on its intent and preserves the invitation field', async () => {
    apiMocks.register.mockRejectedValue({ response: { data: { error: '邀请码无效' } } })
    const source = 'https://source.example/feed'
    renderAuth(`/register?intent=subscribe&source=${encodeURIComponent(source)}`)
    submitRegistration()
    expect(await screen.findByText('邀请码无效')).toBeTruthy()
    expect(screen.getByTestId('location').textContent).toBe(`/register?intent=subscribe&source=${encodeURIComponent(source)}`)
    expect((screen.getByPlaceholderText('邀请码') as HTMLInputElement).value).toBe('invite-42')
    expect(document.body.textContent).not.toContain('secret-password')
  })
})

describe('auth intent URL safety', () => {
  it.each([
    'javascript:alert(1)',
    'data:text/html,bad',
    'file:///etc/passwd',
    '/relative/feed',
    '//evil.example/feed',
    'https://user:secret@source.example/feed',
    'http://localhost/feed',
    'http://localhost./feed',
    'https://preview.localhost/feed',
    'http://10.1.2.3/feed',
    'http://127.0.0.1/feed',
    'http://[::1]/feed',
    'https://8.8.8.8/feed',
    'https://[2606:4700:4700::1111]/feed',
  ])('rejects a non-public subscription source: %s', source => {
    expect(parseAuthIntent(`?intent=subscribe&source=${encodeURIComponent(source)}`)).toEqual({
      kind: 'use',
      returnTo: '/articles',
    })
  })

  it('rejects duplicate, malformed, raw-oversized, and encoded-oversized source parameters', () => {
    const duplicate = '?intent=subscribe&source=https%3A%2F%2Fa.example%2F&source=https%3A%2F%2Fb.example%2F'
    const malformed = '?intent=subscribe&source=%E0%A4%A'
    const rawOversized = `?intent=subscribe&source=${encodeURIComponent(`https://source.example/${'x'.repeat(2049)}`)}`
    const encodedOversized = `?intent=subscribe&source=${encodeURIComponent(`https://source.example/${'中'.repeat(700)}`)}`
    for (const search of [duplicate, malformed, rawOversized, encodedOversized]) {
      expect(parseAuthIntent(search).kind).toBe('use')
    }
    expect(parseAuthIntent('?intent=subscribe&intent=use&source=https%3A%2F%2Fsource.example')).toEqual({
      kind: 'use', returnTo: '/articles',
    })
  })

  it('trims, normalizes, and round-trips one unicode source with canonical single encoding', () => {
    const parsed = parseAuthIntent(`?source=${encodeURIComponent('  HTTPS://source.example/文章?q=阅读  ')}&intent=subscribe`)
    expect(parsed).toEqual({ kind: 'subscribe', returnTo: '/feeds', source: 'HTTPS://source.example/文章?q=阅读' })
    const search = authSearch(parsed)
    expect(search).toBe('?intent=subscribe&source=HTTPS%3A%2F%2Fsource.example%2F%E6%96%87%E7%AB%A0%3Fq%3D%E9%98%85%E8%AF%BB')
    expect(parseAuthIntent(search)).toEqual(parsed)
    expect(postAuthURL(parsed)).toBe('/feeds?add=1&source=HTTPS%3A%2F%2Fsource.example%2F%E6%96%87%E7%AB%A0%3Fq%3D%E9%98%85%E8%AF%BB')
    expect(authSearch({ kind: 'use', returnTo: '/articles' })).toBe('?intent=use')
    expect(postAuthURL({ kind: 'use', returnTo: '/articles' })).toBe('/articles')
  })
})

describe('feed subscription intent', () => {
  it('prefills and previews once, never adds, then replaces the URL', async () => {
    const source = 'https://source.example/feed?q=one'
    const firstLoad = renderFeeds(`/feeds?add=1&source=${encodeURIComponent(source)}`)

    const input = await screen.findByPlaceholderText('输入 RSS 地址、网站 URL 或 PDF 链接') as HTMLInputElement
    await waitFor(() => expect(input.value).toBe(source))
    await waitFor(() => expect(apiMocks.previewFeed).toHaveBeenCalledTimes(1))
    expect(apiMocks.previewFeed).toHaveBeenCalledWith(source)
    expect(apiMocks.addFeed).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/feeds'))

    firstLoad.unmount()
    renderFeeds('/feeds')
    await screen.findByPlaceholderText('输入 RSS 地址、网站 URL 或 PDF 链接')
    expect(apiMocks.previewFeed).toHaveBeenCalledTimes(1)
  })

  it('does not duplicate the automatic preview in StrictMode', async () => {
    const source = 'https://source.example/feed'
    renderFeeds(`/feeds?add=1&source=${encodeURIComponent(source)}`, true)
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/feeds'))
    expect(apiMocks.previewFeed).toHaveBeenCalledTimes(1)
  })

  it.each([
    '/feeds?add=1&source=http%3A%2F%2F10.0.0.1%2Ffeed',
    '/feeds?add=1&source=javascript%3Aalert(1)',
    '/feeds?add=1&source=%E0%A4%A',
    '/feeds?add=1&source=https%3A%2F%2Fa.example&source=https%3A%2F%2Fb.example',
    '/feeds?add=0&source=https%3A%2F%2Fsource.example%2Ffeed',
  ])('cleans an invalid automatic-preview query without fetching it: %s', async path => {
    renderFeeds(path)
    await screen.findByPlaceholderText('输入 RSS 地址、网站 URL 或 PDF 链接')
    await waitFor(() => expect(screen.getByTestId('location').textContent).toBe('/feeds'))
    expect(apiMocks.previewFeed).not.toHaveBeenCalled()
  })

  it('keeps the prefilled source editable and retryable when preview rejects', async () => {
    apiMocks.previewFeed.mockRejectedValue(new Error('network failed'))
    const source = 'https://source.example/feed'
    renderFeeds(`/feeds?add=1&source=${encodeURIComponent(source)}`)
    expect(await screen.findByText('无法获取该地址的内容，请检查 URL 是否正确')).toBeTruthy()
    const input = screen.getByPlaceholderText('输入 RSS 地址、网站 URL 或 PDF 链接') as HTMLInputElement
    expect(input.value).toBe(source)
    expect(input.disabled).toBe(false)
    fireEvent.click(screen.getByRole('button', { name: '预览' }))
    await waitFor(() => expect(apiMocks.previewFeed).toHaveBeenCalledTimes(2))
    expect(apiMocks.addFeed).not.toHaveBeenCalled()
  })
})
