import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import IPhoneShortcutInstall from '../src/components/IPhoneShortcutInstall'
import installPage from '../public/shortcuts/iphone-setup.html?raw'

describe('one-button shortcut installation', () => {
  it('also gives visitors to the old setup URL a direct Apple install button', () => {
    const doc = new DOMParser().parseFromString(installPage, 'text/html')
    const install = doc.querySelector('a[data-shortcut-install]')
    expect(install?.getAttribute('href')).toBe('workflow://shortcuts/bee8c10b751e47a18d87fb75f889b3d3')
    expect(doc.querySelector('ol')).toBeNull()
  })
  it('opens the Apple shortcut installer directly rather than a setup document', () => {
    const url = 'https://www.icloud.com/shortcuts/0123456789abcdef0123456789abcdef'
    render(<IPhoneShortcutInstall url={url} />)
    const install = screen.getByRole('link', { name: '添加到快捷指令' })
    expect(install.getAttribute('href')).toBe('workflow://shortcuts/0123456789abcdef0123456789abcdef')
    expect(screen.getByRole('link', { name: '使用 Apple 安装页' }).getAttribute('href')).toBe(url)
    expect(install.getAttribute('target')).not.toBe('_blank')
    expect(screen.queryByText('手动配置')).toBeNull()
  })

  it.each(['/shortcuts/unsigned.shortcut', '/shortcuts/iphone-setup.html', 'https://example.com/install'])('rejects an unsigned file or documentation link: %s', url => {
    expect(() => IPhoneShortcutInstall({ url })).toThrow('有效的 iCloud 快捷指令安装链接')
  })
})
