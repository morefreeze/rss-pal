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
      '📅简报',
      '⋯更多',
    ])

    fireEvent.click(within(nav).getByRole('button', { name: '更多' }))
    const sheet = screen.getByRole('dialog', { name: '更多' })
    expect(within(sheet).getAllByRole('button').map(item => item.textContent?.trim())).toEqual([
      '💡兴趣',
      '📊统计',
      '⚙️设置',
      '🚪登出',
    ])
  })
})
