import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'
import MobileTabBar from '../src/components/MobileTabBar'

describe('MobileTabBar', () => {
  it('shows primary and overflow destinations in the requested order', () => {
    render(
      <MemoryRouter initialEntries={['/articles']}>
        <MobileTabBar unreadCount={0} onLogout={() => {}} />
      </MemoryRouter>,
    )

    const nav = screen.getByRole('navigation', { name: '主导航' })
    expect(Array.from(nav.querySelectorAll('a, button')).map(item => item.textContent?.trim())).toEqual([
      '📰文章',
      '⭐网摘',
      '📡订阅',
      '🔭探索',
      '⋯更多',
    ])

    fireEvent.click(within(nav).getByRole('button', { name: '更多' }))
    const sheet = screen.getByRole('dialog', { name: '更多' })
    expect(within(sheet).getAllByRole('button').map(item => item.textContent?.trim())).toEqual([
      '📅简报',
      '💡兴趣',
      '📊统计',
      '⚙️设置',
      '🚪登出',
    ])
  })
})

it('管理员在手机更多菜单中可进入后台管理', () => {
  render(<MemoryRouter><MobileTabBar unreadCount={0} onLogout={() => {}} isAdmin /></MemoryRouter>)
  fireEvent.click(screen.getByRole('button', { name: '更多' }))
  expect(screen.getByRole('button', { name: /后台管理/ })).toBeTruthy()
})

it('passes QQ profile through the actual mobile More panel',()=>{
 render(<MemoryRouter><MobileTabBar unreadCount={0} onLogout={()=>{}} user={{username:'reader',nickname:'原昵称',qq_nickname:'QQ昵称',qq_avatar_url:'https://q.qlogo.cn/qqapp/id/40'}} /></MemoryRouter>)
 fireEvent.click(screen.getByRole('button',{name:'更多'}))
 const panel=screen.getByRole('dialog',{name:'更多'})
 expect(within(panel).getByText('原昵称')).toBeTruthy()
 expect(within(panel).getByText('（QQ：QQ昵称）')).toBeTruthy()
 expect(within(panel).getByRole('img',{name:'QQ 头像'})).toBeTruthy()
})
