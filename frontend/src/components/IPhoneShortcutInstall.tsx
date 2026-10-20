/** The URL must be an actual published Apple installer, never a setup guide. */
export const IPHONE_SHORTCUT_INSTALL_URL = 'https://www.icloud.com/shortcuts/fcb8824905304acb9cb0e272f8b7e5c0'

export default function IPhoneShortcutInstall({ url = IPHONE_SHORTCUT_INSTALL_URL }: { url?: string } = {}) {
  if (!/^https:\/\/www\.icloud\.com\/shortcuts\/[a-f0-9]{32}$/i.test(url)) {
    throw new Error('需要有效的 iCloud 快捷指令安装链接')
  }
  // Apple's own Get Shortcut handler uses this scheme for current iOS/macOS.
  const nativeURL = `workflow://shortcuts/${url.slice(url.lastIndexOf('/') + 1)}`

  return <div className="mb-2">
    <a href={nativeURL} style={{
      display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
      minHeight: 44, padding: '8px 16px', borderRadius: 8,
      background: 'var(--fg, #222)', color: 'var(--surface, #fff)',
      textDecoration: 'none', fontWeight: 600,
    }}>添加到快捷指令</a>
    <p className="text-muted text-sm" style={{ marginTop: 8 }}>
      在系统页面确认添加后，返回这里连接当前账号。已有旧版快捷指令时，请替换为新版。
    </p>
    <a href={url} className="text-sm">使用 Apple 安装页</a>
  </div>
}
