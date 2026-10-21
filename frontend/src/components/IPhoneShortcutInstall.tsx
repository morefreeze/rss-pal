/** The URL must be an actual published Apple installer, never a setup guide. */
export const IPHONE_SHORTCUT_INSTALL_URL = 'https://www.icloud.com/shortcuts/98ef2f6cfbe740e092908ab72ea9240e'
// Change only after downloading and inspecting the published Apple archive.
export const IPHONE_SHORTCUT_SUPPORTS_CAPTURE_NOTES = false

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
      在系统页面确认添加后，返回这里连接当前账号。共享链接后，回原文章确认并点“开始抓取”。已有旧版快捷指令时，请替换为新版。
    </p>
    {!IPHONE_SHORTCUT_SUPPORTS_CAPTURE_NOTES && <p className="text-muted text-sm">当前安装版只保存链接；每次共享填写想法的新版尚未发布。</p>}
    <a href={url} className="text-sm">使用 Apple 安装页</a>
  </div>
}
