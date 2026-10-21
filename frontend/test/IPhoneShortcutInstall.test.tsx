import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import IPhoneShortcutInstall, { IPHONE_SHORTCUT_SUPPORTS_CAPTURE_NOTES } from '../src/components/IPhoneShortcutInstall'
import installPage from '../public/shortcuts/iphone-setup.html?raw'

describe('one-button shortcut installation', () => {
  it('also gives visitors to the old setup URL a direct Apple install button', () => {
    const doc = new DOMParser().parseFromString(installPage, 'text/html')
    const install = doc.querySelector('a[data-shortcut-install]')
    expect(install?.getAttribute('href')).toBe('workflow://shortcuts/98ef2f6cfbe740e092908ab72ea9240e')
    expect(doc.querySelector('ol')).toBeNull()
    expect(doc.body.textContent).toContain('把链接加入原文章的待抓取列表')
    expect(doc.body.textContent).toContain('再回原文章确认并点“开始抓取”')
    expect(doc.body.textContent).toContain('当前安装版只保存链接')
  })
  it('opens the Apple shortcut installer directly rather than a setup document', () => {
    const url = 'https://www.icloud.com/shortcuts/0123456789abcdef0123456789abcdef'
    render(<IPhoneShortcutInstall url={url} />)
    const install = screen.getByRole('link', { name: '添加到快捷指令' })
    expect(install.getAttribute('href')).toBe('workflow://shortcuts/0123456789abcdef0123456789abcdef')
    expect(screen.getByRole('link', { name: '使用 Apple 安装页' }).getAttribute('href')).toBe(url)
    expect(install.getAttribute('target')).not.toBe('_blank')
    expect(screen.queryByText('手动配置')).toBeNull()
    expect(screen.getByText(/共享链接后，回原文章确认并点“开始抓取”/)).toBeTruthy()
    expect(IPHONE_SHORTCUT_SUPPORTS_CAPTURE_NOTES).toBe(false)
    expect(screen.getByText(/当前安装版只保存链接/)).toBeTruthy()
  })

  it.each(['/shortcuts/unsigned.shortcut', '/shortcuts/iphone-setup.html', 'https://example.com/install'])('rejects an unsigned file or documentation link: %s', url => {
    expect(() => IPhoneShortcutInstall({ url })).toThrow('有效的 iCloud 快捷指令安装链接')
  })
})
