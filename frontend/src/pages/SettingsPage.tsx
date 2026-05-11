import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import { getTemplates, createTemplate, deleteTemplate, getAIConfig, saveAIConfig, setDefaultTemplate, createInviteCode, getInviteCodes, changePassword, polishPrompt, getBookmarkletToken, regenerateBookmarkletToken, listBackups, createBackupNow, restoreBackup, BackupFile, SummaryTemplate, UserAIConfig, InviteCode } from '../api/client'
import { toast } from '../utils/toast'
import { useReaderSettings } from '../hooks/useReaderSettings'
import { useTheme, type Theme } from '../util/theme'

const STYLE_OPTIONS = [
  { value: 'bullets', label: '要点列表' },
  { value: 'analysis', label: '深度分析' },
  { value: 'oneliner', label: '一句话摘要' },
  { value: 'casual', label: '轻松口语' },
  { value: 'academic', label: '学术风格' },
]

interface SettingsPageProps {
  user?: { is_admin: boolean } | null
}

function PromptField({
  label, value, onChange, rows = 3
}: {
  label: string
  value: string
  onChange: (v: string) => void
  rows?: number
}) {
  const [polishing, setPolishing] = useState(false)
  const [polished, setPolished] = useState<string | null>(null)

  const handlePolish = async () => {
    if (!value.trim()) {
      toast.error('请先填写 Prompt 内容')
      return
    }
    setPolishing(true)
    setPolished(null)
    try {
      const result = await polishPrompt(value)
      setPolished(result)
    } catch {
      toast.error('AI 润色失败，请重试')
    } finally {
      setPolishing(false)
    }
  }

  return (
    <div className="mb-1">
      <div className="flex-between" style={{ marginBottom: 4 }}>
        <label className="text-sm text-bold">{label}</label>
        <button
          type="button"
          className="secondary"
          style={{ fontSize: 11, padding: '2px 8px' }}
          onClick={handlePolish}
          disabled={polishing}
        >
          {polishing ? '润色中...' : '✨ AI 润色'}
        </button>
      </div>
      <textarea
        value={value}
        onChange={e => onChange(e.target.value)}
        rows={rows}
        style={{ width: '100%' }}
      />
      {polished !== null && (
        <div style={{ marginTop: 6, padding: 10, background: '#f0fdf4', borderRadius: 6, border: '1px solid #bbf7d0' }}>
          <div className="text-sm text-bold" style={{ marginBottom: 4, color: '#16a34a' }}>润色结果：</div>
          <div className="text-sm" style={{ whiteSpace: 'pre-wrap', marginBottom: 8 }}>{polished}</div>
          <div className="flex gap-2">
            <button type="button" style={{ fontSize: 12, padding: '3px 10px' }} onClick={() => { onChange(polished); setPolished(null) }}>
              使用润色版
            </button>
            <button type="button" className="secondary" style={{ fontSize: 12, padding: '3px 10px' }} onClick={() => setPolished(null)}>
              取消
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function buildBookmarkletJS(apiBase: string, token: string): string {
  // We open a same-origin receiver page on the RSS Pal host and let it POST
  // to /api/bookmarklet/capture from its own origin (no foreign-CSP / mixed-
  // content issues). The payload is base64-encoded into the receiver's URL
  // fragment — fragments are client-only (never sent over the wire) and
  // survive Cross-Origin-Opener-Policy: same-origin (set by sites like
  // rockpapershotgun.com), which severs window.opener and broke an earlier
  // postMessage-handshake design.
  //
  // Pre-trim on a clone of documentElement: drops script/style/link/noscript
  // (≈60× shrink on mp.weixin.qq.com — 3 MiB → 50 KiB — also keeping the
  // resulting URL fragment well under browser limits) and promotes data-src
  // into src so lazy-loaded article images survive the round trip. The live
  // DOM is not mutated.
  const code = `(function(){
var c=document.documentElement.cloneNode(true);
c.querySelectorAll('script,style,link,noscript,template').forEach(function(n){n.remove();});
c.querySelectorAll('img').forEach(function(i){
  var s=(i.getAttribute('src')||'').trim();
  if(!s||s.indexOf('data:')===0){
    var lazy=i.getAttribute('data-src')||i.getAttribute('data-original')||i.getAttribute('data-actual-src')||i.getAttribute('data-lazy-src')||i.getAttribute('data-original-src');
    if(lazy)i.setAttribute('src',lazy.trim());
  }
  if(!i.getAttribute('srcset')){
    var ss=i.getAttribute('data-srcset')||i.getAttribute('data-lazy-srcset');
    if(ss)i.setAttribute('srcset',ss.trim());
  }
});
var payload=JSON.stringify({token:'${token}',url:location.href,title:document.title,html:c.outerHTML});
var b64;
try{b64=btoa(unescape(encodeURIComponent(payload)));}catch(e){alert('RSS Pal: 编码失败 '+e.message);return;}
var w=window.open('${apiBase}/bookmarklet-receiver.html#'+b64,'_blank');
if(!w){alert('RSS Pal: 请允许此页面弹窗后再试');return;}
})();`
  return 'javascript:' + encodeURIComponent(code)
}

function BackupSection({ isAdmin }: { isAdmin: boolean }) {
  const [files, setFiles] = useState<BackupFile[]>([])
  const [dir, setDir] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const data = await listBackups()
      setFiles(data.backups || [])
      setDir(data.dir || '')
    } catch (err: any) {
      if (err?.response?.status === 403) setError('需要管理员权限')
      else setError('加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (isAdmin) load()
    else setLoading(false)
  }, [isAdmin])

  const handleBackupNow = async () => {
    setBusy(true)
    try {
      await createBackupNow()
      toast.success('备份完成')
      await load()
    } catch {
      toast.error('备份失败')
    } finally {
      setBusy(false)
    }
  }

  const handleRestore = async (name: string) => {
    const ok = window.confirm(
      `确定从 ${name} 恢复吗？\n\n` +
      `- 现有订阅按 URL 合并（已存在则保留当前状态，仅补缺）\n` +
      `- 标签 / 兴趣分类 / 兴趣主题会按自然键 upsert\n` +
      `- 文章相关的 tag/preference 不会恢复（文章会重新抓）`
    )
    if (!ok) return
    setBusy(true)
    try {
      const result = await restoreBackup(name)
      const s = result.stats
      toast.success(`已恢复：${s.feeds} feeds / ${s.user_tags} tags / ${s.interest_categories} cats / ${s.interest_topics} topics`)
      if (s.skipped_article_link > 0) {
        toast.info(`${s.skipped_article_link} 条 article-linked 记录已跳过（无法重新关联）`)
      }
    } catch {
      toast.error('恢复失败')
    } finally {
      setBusy(false)
    }
  }

  if (!isAdmin) return null

  return (
    <div className="card mb-2">
      <div className="flex-between mb-1">
        <h3>备份与恢复</h3>
        <button onClick={handleBackupNow} disabled={busy} style={{ fontSize: 13 }}>
          {busy ? '...' : '立即备份'}
        </button>
      </div>
      <p className="text-muted text-sm mb-2">
        快照内容：订阅源、标签、兴趣分类与主题、用户偏好。文章本体不在备份内（worker 会重新抓）。
        文件存在主机 <code style={{ background: 'var(--code-bg)', padding: '1px 6px', borderRadius: 3 }}>./backups/</code>
        （容器内 <code style={{ background: 'var(--code-bg)', padding: '1px 6px', borderRadius: 3 }}>{dir || '/backups'}</code>），
        每次 feed 增删后 5 分钟自动备份，每日定时一次；保留策略 7 天 / 周 / 月分级。
      </p>
      {loading ? (
        <div className="text-muted text-sm">加载中...</div>
      ) : error ? (
        <div className="text-sm" style={{ color: '#dc2626' }}>{error}</div>
      ) : files.length === 0 ? (
        <div className="text-muted text-sm">还没有备份文件，点击"立即备份"创建第一份</div>
      ) : (
        <div>
          {files.map(f => (
            <div key={f.name} className="flex-between" style={{ padding: '6px 0', borderBottom: '1px solid var(--border)' }}>
              <div>
                <code style={{ background: 'var(--code-bg)', padding: '2px 8px', borderRadius: 4, fontSize: 12 }}>
                  {f.name}
                </code>
                <span className="text-muted text-sm" style={{ marginLeft: 8 }}>
                  {new Date(f.created_at).toLocaleString('zh-CN')} · {(f.size / 1024).toFixed(1)} KB
                </span>
              </div>
              <button
                className="secondary"
                style={{ fontSize: 12, padding: '2px 10px' }}
                onClick={() => handleRestore(f.name)}
                disabled={busy}
              >
                恢复
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function BookmarkletSection() {
  const [token, setToken] = useState<string | null>(null)
  const [revealed, setRevealed] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    getBookmarkletToken().then(setToken).catch(() => setToken(null))
  }, [])

  const handleRegenerate = async () => {
    if (token && !confirm('重新生成会让旧书签失效，确认?')) return
    setBusy(true)
    try {
      const t = await regenerateBookmarkletToken()
      setToken(t)
      setRevealed(true)
      toast.success('Token 已生成，请重新拖动书签')
    } catch {
      toast.error('生成失败，请重试')
    } finally {
      setBusy(false)
    }
  }

  const apiBase = window.location.origin
  const bookmarkletJS = token ? buildBookmarkletJS(apiBase, token) : ''
  const masked = token ? token.slice(0, 6) + '…' + token.slice(-4) : '尚未生成'

  return (
    <div className="card mb-2">
      <h3 className="mb-1">📌 浏览器抓取</h3>
      <p className="text-muted text-sm mb-2">
        把下方按钮拖到浏览器书签栏。在任何网页点一下，就把当前页发回 RSS Pal —
        匹配到已有文章则更新内容，否则保存到「⭐ 网摘」feed。结果会在新标签页提示，成功后自动关闭。
      </p>

      {token ? (
        <div style={{ marginBottom: 12 }}>
          <a
            href={bookmarkletJS}
            draggable
            onClick={e => e.preventDefault()}
            style={{
              display: 'inline-block',
              padding: '8px 16px',
              background: '#222',
              color: '#fff',
              borderRadius: 6,
              textDecoration: 'none',
              fontSize: 14,
              cursor: 'grab',
            }}
          >
            ⭐ 发送到 RSS Pal
          </a>
          <span className="text-muted text-sm" style={{ marginLeft: 12 }}>
            ← 拖到书签栏
          </span>
        </div>
      ) : (
        <div className="text-muted text-sm mb-2">点「生成 Token」获取你的第一个 token。</div>
      )}

      <div className="flex gap-2" style={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <span className="text-sm">Token:</span>
        <code style={{ background: 'var(--code-bg)', padding: '3px 8px', borderRadius: 4, fontSize: 12 }}>
          {revealed && token ? token : masked}
        </code>
        {token && (
          <button
            type="button"
            className="secondary"
            style={{ fontSize: 12, padding: '3px 10px' }}
            onClick={() => setRevealed(v => !v)}
          >
            {revealed ? '隐藏' : '显示'}
          </button>
        )}
        <button
          type="button"
          style={{ fontSize: 12, padding: '3px 10px' }}
          onClick={handleRegenerate}
          disabled={busy}
        >
          {busy ? '...' : token ? '🔄 重新生成' : '生成 Token'}
        </button>
      </div>
      {token && (
        <p className="text-muted text-sm" style={{ marginTop: 8, marginBottom: 0 }}>
          ⚠️ 重新生成后旧书签会失效，需要重新拖一次。
        </p>
      )}
    </div>
  )
}

type TabId = 'appearance' | 'account' | 'ai' | 'tools'

const TABS: { id: TabId; label: string }[] = [
  { id: 'appearance', label: '外观' },
  { id: 'account',    label: '账号' },
  { id: 'ai',         label: 'AI' },
  { id: 'tools',      label: '工具' },
]

function readTabFromHash(): TabId {
  const h = window.location.hash.replace(/^#/, '')
  if (h === 'appearance' || h === 'account' || h === 'ai' || h === 'tools') return h
  return 'appearance'
}

function useSettingsTab(): [TabId, (t: TabId) => void] {
  const [tab, setTabState] = useState<TabId>(() => readTabFromHash())
  useEffect(() => {
    // Clean up a stale invalid hash on first mount so /settings#foo silently
    // becomes /settings#appearance instead of leaving #foo in the address bar.
    const raw = window.location.hash.slice(1)
    if (raw && raw !== tab) {
      history.replaceState(null, '', `${window.location.pathname}#${tab}`)
    }
    const onHash = () => setTabState(readTabFromHash())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  // Single source of truth: write the hash and let `hashchange` drive state.
  const setTab = useCallback((t: TabId) => {
    window.location.hash = t
  }, [])
  return [tab, setTab]
}

function SettingsTabs({ tab, onTab }: { tab: TabId; onTab: (t: TabId) => void }) {
  return (
    <div className="settings-tabs" role="tablist">
      {TABS.map(t => (
        <button
          key={t.id}
          role="tab"
          aria-selected={tab === t.id}
          className={tab === t.id ? 'settings-tab active' : 'settings-tab'}
          onClick={() => onTab(t.id)}
        >
          {t.label}
        </button>
      ))}
    </div>
  )
}

const THEME_META: { id: Theme; label: string; bg: string; fg: string; accent: string; border: string }[] = [
  { id: 'paper', label: 'Paper · 暖纸',  bg: '#f5edd6', fg: '#3a2f1a', accent: '#5b3b00', border: '#d9cdaa' },
  { id: 'quiet', label: 'Quiet · 静白',  bg: '#fafaf7', fg: '#1f2933', accent: '#3a5a40', border: '#e3e3de' },
  { id: 'pearl', label: 'Pearl · 珍珠',  bg: '#ebebeb', fg: '#262626', accent: '#0066cc', border: '#d4d4d4' },
  { id: 'night', label: 'Night · 夜读',  bg: '#1a1a1a', fg: '#d4d4d4', accent: '#7eb6ff', border: '#333333' },
]

function ThemePicker() {
  const [theme, setTheme] = useTheme()
  return (
    <div>
      <div className="theme-grid">
        {THEME_META.map(t => (
          <button
            key={t.id}
            type="button"
            className={theme === t.id ? 'theme-swatch active' : 'theme-swatch'}
            onClick={() => setTheme(t.id)}
            aria-pressed={theme === t.id}
          >
            <span
              className="theme-swatch-preview"
              style={{ background: t.bg, color: t.fg, borderColor: t.border }}
            >
              <span className="swatch-title">Aa</span>
              <span className="swatch-dot" style={{ background: t.accent }} />
            </span>
            <span>{t.label}</span>
          </button>
        ))}
      </div>
      <div className="theme-preview">
        <h3 style={{ marginBottom: 6 }}>预览</h3>
        <p className="mb-1">这是一段示例正文，用来预览当前主题下的对比度和阅读感。</p>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center' }}>
          <button type="button">主按钮</button>
          <button type="button" className="btn-ghost">次按钮</button>
          <button type="button" className="btn-text">文本按钮</button>
          <code style={{ background: 'var(--code-bg)', padding: '2px 6px', borderRadius: 3, fontSize: 13 }}>
            const x = 1
          </code>
        </div>
      </div>
    </div>
  )
}

function AppearanceTab({ readerSettings }: { readerSettings: ReturnType<typeof useReaderSettings> }) {
  return (
    <>
      <div className="card mb-2">
        <h3 className="mb-2">主题</h3>
        <p className="text-muted text-sm mb-2">主题作用于整个站点，包括文章阅读视图。</p>
        <ThemePicker />
      </div>

      <div className="card mb-2">
        <h3 className="mb-1">阅读体验</h3>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <span className="text-sm">字号：</span>
          <button className="btn-ghost btn-sm" onClick={() => readerSettings.setFontSize(readerSettings.fontSize - 1)} disabled={readerSettings.fontSize <= 12}>A−</button>
          <span className="text-sm" style={{ minWidth: 48, textAlign: 'center' }}>{readerSettings.fontSize} px</span>
          <button className="btn-ghost btn-sm" onClick={() => readerSettings.setFontSize(readerSettings.fontSize + 1)} disabled={readerSettings.fontSize >= 24}>A+</button>
        </div>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center' }}>
          <span className="text-sm">字体：</span>
          <button
            className={readerSettings.fontFamily === 'sans' ? 'btn-sm' : 'btn-ghost btn-sm'}
            onClick={() => readerSettings.setFontFamily('sans')}
          >Sans</button>
          <button
            className={readerSettings.fontFamily === 'serif' ? 'btn-sm' : 'btn-ghost btn-sm'}
            onClick={() => readerSettings.setFontFamily('serif')}
            style={{ fontFamily: 'var(--font-serif)' }}
          >Serif</button>
        </div>
        <div className="flex gap-2 mt-2" style={{ alignItems: 'center' }}>
          <label className="text-sm flex gap-1" style={{ alignItems: 'center' }}>
            <input
              type="checkbox"
              style={{ width: 'auto' }}
              checked={readerSettings.confettiEnabled}
              onChange={e => readerSettings.setConfettiEnabled(e.target.checked)}
            />
            阅读完成时显示彩带
          </label>
        </div>
      </div>
    </>
  )
}

export default function SettingsPage({ user }: SettingsPageProps) {
  const readerSettings = useReaderSettings()
  const [tab, setTab] = useSettingsTab()
  const [templates, setTemplates] = useState<SummaryTemplate[]>([])
  const [aiConfig, setAiConfig] = useState<UserAIConfig>({ api_key: '', base_url: '', model: '' })
  const [apiKeyConfigured, setApiKeyConfigured] = useState(false)
  const [loading, setLoading] = useState(true)
  const [aiSaving, setAiSaving] = useState(false)
  const [aiError, setAiError] = useState('')
  const [aiSuccess, setAiSuccess] = useState('')

  // Invite codes (admin only)
  const [inviteCodes, setInviteCodes] = useState<InviteCode[]>([])
  const [inviteLoading, setInviteLoading] = useState(false)
  const [inviteCreating, setInviteCreating] = useState(false)
  const [copiedCode, setCopiedCode] = useState('')

  // Password change
  const [oldPassword, setOldPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [pwChanging, setPwChanging] = useState(false)
  const [pwError, setPwError] = useState('')
  const [pwSuccess, setPwSuccess] = useState('')

  const [showNewTemplate, setShowNewTemplate] = useState(false)
  const [newTemplate, setNewTemplate] = useState<Partial<SummaryTemplate>>({
    name: '',
    description: '',
    brief_prompt: '',
    detailed_prompt: '',
    style: 'bullets',
  })
  const [templateSaving, setTemplateSaving] = useState(false)
  const [templateError, setTemplateError] = useState('')

  // Which system templates have their prompts expanded
  const [expandedTemplates, setExpandedTemplates] = useState<Set<number>>(new Set())

  useEffect(() => {
    const load = async () => {
      try {
        const [tmpl, cfg] = await Promise.all([getTemplates(), getAIConfig()])
        setTemplates(tmpl || [])
        if (cfg) {
          const hasKey = !!cfg.api_key
          setApiKeyConfigured(hasKey)
          setAiConfig({ api_key: '', base_url: cfg.base_url || '', model: cfg.model || '' })
        }
      } catch {
        // ignore — backend may not have config yet
      } finally {
        setLoading(false)
      }
    }
    load()
  }, [])

  useEffect(() => {
    if (!user?.is_admin) return
    setInviteLoading(true)
    getInviteCodes().then(codes => setInviteCodes(codes || [])).catch(() => {}).finally(() => setInviteLoading(false))
  }, [user])

  const handleCreateInviteCode = async () => {
    setInviteCreating(true)
    try {
      const code = await createInviteCode(72)
      setInviteCodes(prev => [code, ...prev])
    } catch {
      toast.error('创建邀请码失败')
    } finally {
      setInviteCreating(false)
    }
  }

  const handleCopyCode = (code: string) => {
    navigator.clipboard.writeText(code).then(() => {
      setCopiedCode(code)
      setTimeout(() => setCopiedCode(''), 2000)
    })
  }

  const handleSaveAI = async () => {
    setAiSaving(true)
    setAiError('')
    setAiSuccess('')
    try {
      await saveAIConfig(aiConfig)
      if (aiConfig.api_key) {
        setApiKeyConfigured(true)
        setAiConfig(prev => ({ ...prev, api_key: '' }))
      }
      setAiSuccess('保存成功')
    } catch {
      setAiError('保存失败，请重试')
    } finally {
      setAiSaving(false)
    }
  }

  const handleClearAI = async () => {
    if (!confirm('确定清除 AI 配置？将恢复使用系统 AI')) return
    setAiSaving(true)
    setAiError('')
    setAiSuccess('')
    try {
      await saveAIConfig({ api_key: '', base_url: '', model: '' })
      setAiConfig({ api_key: '', base_url: '', model: '' })
      setApiKeyConfigured(false)
      setAiSuccess('已清除，将使用系统 AI')
    } catch {
      setAiError('操作失败，请重试')
    } finally {
      setAiSaving(false)
    }
  }

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault()
    if (newPassword.length < 6) {
      setPwError('新密码至少 6 位')
      return
    }
    setPwChanging(true)
    setPwError('')
    setPwSuccess('')
    try {
      await changePassword(oldPassword, newPassword)
      setOldPassword('')
      setNewPassword('')
      setPwSuccess('密码已修改')
    } catch (err: any) {
      setPwError(err?.response?.data?.error || '修改失败，请重试')
    } finally {
      setPwChanging(false)
    }
  }

  const handleDeleteTemplate = async (id: number) => {
    if (!confirm('确定删除此模板？')) return
    try {
      await deleteTemplate(id)
      setTemplates(prev => prev.filter(t => t.id !== id))
    } catch {
      toast.error('删除失败')
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await setDefaultTemplate(id)
      toast.success('已设为默认模板')
    } catch {
      toast.error('设置失败')
    }
  }

  const handleCreateTemplate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!newTemplate.name) {
      setTemplateError('请填写模板名称')
      return
    }
    setTemplateSaving(true)
    setTemplateError('')
    try {
      const created = await createTemplate(newTemplate)
      setTemplates(prev => [...prev, created])
      setNewTemplate({ name: '', description: '', brief_prompt: '', detailed_prompt: '', style: 'bullets' })
      setShowNewTemplate(false)
    } catch {
      setTemplateError('创建失败，请重试')
    } finally {
      setTemplateSaving(false)
    }
  }

  const handleCopySystemTemplate = (t: SummaryTemplate) => {
    setNewTemplate({
      name: `${t.name}（副本）`,
      description: t.description,
      brief_prompt: t.brief_prompt,
      detailed_prompt: t.detailed_prompt,
      style: t.style,
    })
    setShowNewTemplate(true)
    setTimeout(() => {
      document.querySelector<HTMLElement>('[data-new-template-form]')?.scrollIntoView({ behavior: 'smooth' })
    }, 50)
  }

  const toggleExpand = (id: number) => {
    setExpandedTemplates(prev => {
      const next = new Set(prev)
      next.has(id) ? next.delete(id) : next.add(id)
      return next
    })
  }

  if (loading) return <div className="card">Loading...</div>

  const systemTemplates = templates.filter(t => t.is_system)
  const userTemplates = templates.filter(t => !t.is_system)

  const formatDate = (d: string | null) => d ? new Date(d).toLocaleString('zh-CN') : '永不'

  return (
    <div>
      <h2 className="mb-2">设置</h2>
      <SettingsTabs tab={tab} onTab={setTab} />

      {tab === 'appearance' && (
        <AppearanceTab readerSettings={readerSettings} />
      )}

      {tab === 'account' && (
        <>
          {/* 修改密码 */}
          <div className="card mb-2">
            <h3 className="mb-2">修改密码</h3>
            <form onSubmit={handleChangePassword}>
              <div className="mb-1">
                <input
                  type="password"
                  value={oldPassword}
                  placeholder="当前密码"
                  onChange={e => setOldPassword(e.target.value)}
                  style={{ width: '100%' }}
                  disabled={pwChanging}
                />
              </div>
              <div className="mb-2">
                <input
                  type="password"
                  value={newPassword}
                  placeholder="新密码（至少 6 位）"
                  onChange={e => setNewPassword(e.target.value)}
                  style={{ width: '100%' }}
                  disabled={pwChanging}
                />
              </div>
              {pwError && <div className="text-sm mb-1" style={{ color: '#dc2626' }}>{pwError}</div>}
              {pwSuccess && <div className="text-sm mb-1" style={{ color: '#16a34a' }}>{pwSuccess}</div>}
              <button type="submit" disabled={pwChanging || !oldPassword || !newPassword}>
                {pwChanging ? '修改中...' : '修改密码'}
              </button>
            </form>
          </div>

          {/* 邀请码管理（仅管理员可见） */}
          {user?.is_admin && (
            <div className="card mb-2">
              <div className="flex-between mb-1">
                <h3>邀请码管理</h3>
                <button onClick={handleCreateInviteCode} disabled={inviteCreating}>
                  {inviteCreating ? '创建中...' : '生成邀请码'}
                </button>
              </div>
              <p className="text-muted text-sm mb-2">最多允许 10 名测试用户注册</p>
              {inviteLoading ? (
                <div className="text-muted text-sm">加载中...</div>
              ) : inviteCodes.length === 0 ? (
                <div className="text-muted text-sm">暂无邀请码，点击"生成邀请码"创建</div>
              ) : (
                <div>
                  {inviteCodes.map(ic => (
                    <div key={ic.id} className="flex-between" style={{ padding: '6px 0', borderBottom: '1px solid var(--border)' }}>
                      <div>
                        <code style={{ fontSize: 14, background: 'var(--code-bg)', padding: '2px 8px', borderRadius: 4 }}>{ic.code}</code>
                        <span className="text-muted text-sm" style={{ marginLeft: 8 }}>
                          {ic.used_by ? <span style={{ color: '#16a34a' }}>已使用</span> : <span style={{ color: 'var(--accent)' }}>未使用</span>}
                        </span>
                        <span className="text-muted text-sm" style={{ marginLeft: 8 }}>
                          过期：{formatDate(ic.expires_at)}
                        </span>
                      </div>
                      {!ic.used_by && (
                        <button className="secondary" style={{ fontSize: 12, padding: '2px 10px' }} onClick={() => handleCopyCode(ic.code)}>
                          {copiedCode === ic.code ? '已复制！' : '复制'}
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </>
      )}

      {tab === 'ai' && (
        <>
          {/* AI 配置区域 */}
          <div className="card mb-2">
            <h3 className="mb-1">我的 AI 配置</h3>
            <p className="text-muted text-sm mb-2">配置你自己的 AI 服务，将优先于系统 AI 使用</p>

            <div className="mb-1">
              <div className="flex-between" style={{ marginBottom: 4 }}>
                <label className="text-sm text-bold">API Key</label>
                {apiKeyConfigured && !aiConfig.api_key && (
                  <span className="text-sm" style={{ color: '#16a34a' }}>✓ 已配置</span>
                )}
              </div>
              <input
                type="password"
                value={aiConfig.api_key}
                placeholder={apiKeyConfigured ? '输入新 Key 以覆盖现有配置' : 'sk-...'}
                onChange={e => setAiConfig(prev => ({ ...prev, api_key: e.target.value }))}
                style={{ width: '100%' }}
              />
            </div>

            <div className="mb-1">
              <label className="text-sm text-bold">Base URL</label>
              <input
                type="text"
                value={aiConfig.base_url}
                placeholder="https://api.openai.com/v1"
                onChange={e => setAiConfig(prev => ({ ...prev, base_url: e.target.value }))}
                style={{ width: '100%', marginTop: 4 }}
              />
            </div>

            <div className="mb-2">
              <label className="text-sm text-bold">Model</label>
              <input
                type="text"
                value={aiConfig.model}
                placeholder="gpt-4o-mini"
                onChange={e => setAiConfig(prev => ({ ...prev, model: e.target.value }))}
                style={{ width: '100%', marginTop: 4 }}
              />
            </div>

            {aiError && <div className="text-sm mb-1" style={{ color: '#dc2626' }}>{aiError}</div>}
            {aiSuccess && <div className="text-sm mb-1" style={{ color: '#16a34a' }}>{aiSuccess}</div>}

            <div className="flex gap-2">
              <button onClick={handleSaveAI} disabled={aiSaving}>
                {aiSaving ? '保存中...' : '保存'}
              </button>
              <button className="secondary" onClick={handleClearAI} disabled={aiSaving}>
                清除配置
              </button>
            </div>
          </div>

          {/* 摘要模板区域 */}
          <div className="card">
            <div className="flex-between mb-2">
              <h3>摘要模板</h3>
              <button onClick={() => setShowNewTemplate(prev => !prev)}>
                {showNewTemplate ? '取消' : '新建模板'}
              </button>
            </div>

            {/* 新建模板表单 */}
            {showNewTemplate && (
              <form data-new-template-form onSubmit={handleCreateTemplate} className="card mb-2" style={{ background: 'var(--surface)' }}>
                <h4 className="mb-1">新建模板</h4>

                <div className="mb-1">
                  <label className="text-sm text-bold">名称 *</label>
                  <input
                    type="text"
                    value={newTemplate.name}
                    onChange={e => setNewTemplate(prev => ({ ...prev, name: e.target.value }))}
                    style={{ width: '100%', marginTop: 4 }}
                  />
                </div>

                <div className="mb-1">
                  <label className="text-sm text-bold">描述</label>
                  <input
                    type="text"
                    value={newTemplate.description}
                    onChange={e => setNewTemplate(prev => ({ ...prev, description: e.target.value }))}
                    style={{ width: '100%', marginTop: 4 }}
                  />
                </div>

                <div className="mb-1">
                  <label className="text-sm text-bold">风格</label>
                  <select
                    value={newTemplate.style}
                    onChange={e => setNewTemplate(prev => ({ ...prev, style: e.target.value }))}
                    style={{ width: '100%', marginTop: 4 }}
                  >
                    {STYLE_OPTIONS.map(opt => (
                      <option key={opt.value} value={opt.value}>{opt.label}</option>
                    ))}
                  </select>
                </div>

                <PromptField
                  label="简短摘要 Prompt"
                  value={newTemplate.brief_prompt || ''}
                  onChange={v => setNewTemplate(prev => ({ ...prev, brief_prompt: v }))}
                  rows={3}
                />

                <PromptField
                  label="详细摘要 Prompt"
                  value={newTemplate.detailed_prompt || ''}
                  onChange={v => setNewTemplate(prev => ({ ...prev, detailed_prompt: v }))}
                  rows={4}
                />

                {templateError && <div className="text-sm mb-1" style={{ color: '#dc2626' }}>{templateError}</div>}

                <button type="submit" disabled={templateSaving}>
                  {templateSaving ? '创建中...' : '创建模板'}
                </button>
              </form>
            )}

            {/* 系统模板 */}
            {systemTemplates.length > 0 && (
              <div className="mb-2">
                <div className="text-sm text-muted mb-1" style={{ fontWeight: 600 }}>系统模板</div>
                {systemTemplates.map(t => (
                  <div key={t.id} style={{ padding: '8px 0', borderBottom: '1px solid var(--border)' }}>
                    <div className="flex-between">
                      <div>
                        <span className="text-bold text-sm">{t.name}</span>
                        {t.style && (
                          <span className="text-sm" style={{ marginLeft: 8, padding: '2px 8px', background: '#e0f2fe', borderRadius: 4, color: '#0369a1' }}>
                            {STYLE_OPTIONS.find(o => o.value === t.style)?.label || t.style}
                          </span>
                        )}
                        {t.description && <div className="text-muted text-sm mt-1">{t.description}</div>}
                      </div>
                      <div className="flex gap-1">
                        <button className="secondary" style={{ fontSize: 12, padding: '2px 8px' }} onClick={() => toggleExpand(t.id)}>
                          {expandedTemplates.has(t.id) ? '收起' : '查看'}
                        </button>
                        <button className="secondary" style={{ fontSize: 12, padding: '2px 8px' }} onClick={() => handleCopySystemTemplate(t)}>
                          复制
                        </button>
                        <button className="secondary" onClick={() => handleSetDefault(t.id)}>设为默认</button>
                      </div>
                    </div>
                    {expandedTemplates.has(t.id) && (
                      <div style={{ marginTop: 8, padding: 10, background: 'var(--surface)', borderRadius: 6, border: '1px solid var(--border)' }}>
                        {t.brief_prompt && (
                          <div className="mb-2">
                            <div className="text-sm text-bold text-muted" style={{ marginBottom: 4 }}>简短摘要 Prompt：</div>
                            <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, margin: 0, color: 'var(--fg)', lineHeight: 1.5 }}>{t.brief_prompt}</pre>
                          </div>
                        )}
                        {t.detailed_prompt && (
                          <div>
                            <div className="text-sm text-bold text-muted" style={{ marginBottom: 4 }}>详细摘要 Prompt：</div>
                            <pre style={{ whiteSpace: 'pre-wrap', fontSize: 12, margin: 0, color: 'var(--fg)', lineHeight: 1.5 }}>{t.detailed_prompt}</pre>
                          </div>
                        )}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}

            {/* 用户自定义模板 */}
            <div>
              <div className="text-sm text-muted mb-1" style={{ fontWeight: 600 }}>自定义模板</div>
              {userTemplates.length === 0 ? (
                <div className="text-muted text-sm">暂无自定义模板</div>
              ) : (
                userTemplates.map(t => (
                  <div key={t.id} className="flex-between" style={{ padding: '8px 0', borderBottom: '1px solid var(--border)' }}>
                    <div>
                      <span className="text-bold text-sm">{t.name}</span>
                      {t.style && (
                        <span className="text-sm" style={{ marginLeft: 8, padding: '2px 8px', background: '#fef9c3', borderRadius: 4, color: '#854d0e' }}>
                          {STYLE_OPTIONS.find(o => o.value === t.style)?.label || t.style}
                        </span>
                      )}
                      {t.description && <div className="text-muted text-sm mt-1">{t.description}</div>}
                    </div>
                    <div className="flex gap-1">
                      <button className="secondary" onClick={() => handleSetDefault(t.id)}>设为默认</button>
                      <button className="secondary" onClick={() => handleDeleteTemplate(t.id)}>删除</button>
                    </div>
                  </div>
                ))
              )}
            </div>
          </div>
        </>
      )}

      {tab === 'tools' && (
        <>
          {/* 工具入口 */}
          <div className="card mb-2">
            <h3 className="mb-1">工具</h3>
            <p className="text-muted text-sm mb-2">订阅源治理与阅读行为分析</p>
            <Link to="/feeds/health">📊 Feed 健康度面板 →</Link>
          </div>

          {/* 浏览器抓取（bookmarklet） */}
          <BookmarkletSection />

          {/* 备份与恢复（仅管理员可见） */}
          <BackupSection isAdmin={!!user?.is_admin} />
        </>
      )}
    </div>
  )
}
