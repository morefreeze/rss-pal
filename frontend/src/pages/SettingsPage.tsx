import { useState, useEffect } from 'react'
import { getTemplates, createTemplate, deleteTemplate, getAIConfig, saveAIConfig, setDefaultTemplate, createInviteCode, getInviteCodes, changePassword, SummaryTemplate, UserAIConfig, InviteCode } from '../api/client'

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

export default function SettingsPage({ user }: SettingsPageProps) {
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

  useEffect(() => {
    const load = async () => {
      try {
        const [tmpl, cfg] = await Promise.all([getTemplates(), getAIConfig()])
        setTemplates(tmpl || [])
        if (cfg) {
          const hasKey = !!cfg.api_key
          setApiKeyConfigured(hasKey)
          // Don't pre-fill masked API key — keep field empty so user enters a new one if changing
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
      alert('创建邀请码失败')
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
      alert('删除失败')
    }
  }

  const handleSetDefault = async (id: number) => {
    try {
      await setDefaultTemplate(id)
      alert('已设为默认模板')
    } catch {
      alert('设置失败')
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

  if (loading) return <div className="card">Loading...</div>

  const systemTemplates = templates.filter(t => t.is_system)
  const userTemplates = templates.filter(t => !t.is_system)

  const formatDate = (d: string | null) => d ? new Date(d).toLocaleString('zh-CN') : '永不'

  return (
    <div>
      <h2 className="mb-2">设置</h2>

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
                <div key={ic.id} className="flex-between" style={{ padding: '6px 0', borderBottom: '1px solid #f0f0f0' }}>
                  <div>
                    <code style={{ fontSize: 14, background: '#f3f4f6', padding: '2px 8px', borderRadius: 4 }}>{ic.code}</code>
                    <span className="text-muted text-sm" style={{ marginLeft: 8 }}>
                      {ic.used_by ? <span style={{ color: '#16a34a' }}>已使用</span> : <span style={{ color: '#2563eb' }}>未使用</span>}
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
          <form onSubmit={handleCreateTemplate} className="card mb-2" style={{ background: '#f8fafc' }}>
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

            <div className="mb-1">
              <label className="text-sm text-bold">简短摘要 Prompt</label>
              <textarea
                value={newTemplate.brief_prompt}
                onChange={e => setNewTemplate(prev => ({ ...prev, brief_prompt: e.target.value }))}
                rows={3}
                style={{ width: '100%', marginTop: 4 }}
              />
            </div>

            <div className="mb-2">
              <label className="text-sm text-bold">详细摘要 Prompt</label>
              <textarea
                value={newTemplate.detailed_prompt}
                onChange={e => setNewTemplate(prev => ({ ...prev, detailed_prompt: e.target.value }))}
                rows={4}
                style={{ width: '100%', marginTop: 4 }}
              />
            </div>

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
              <div key={t.id} className="flex-between" style={{ padding: '8px 0', borderBottom: '1px solid #f0f0f0' }}>
                <div>
                  <span className="text-bold text-sm">{t.name}</span>
                  {t.style && (
                    <span className="text-sm" style={{ marginLeft: 8, padding: '2px 8px', background: '#e0f2fe', borderRadius: 4, color: '#0369a1' }}>
                      {STYLE_OPTIONS.find(o => o.value === t.style)?.label || t.style}
                    </span>
                  )}
                  {t.description && <div className="text-muted text-sm mt-1">{t.description}</div>}
                </div>
                <button className="secondary" onClick={() => handleSetDefault(t.id)}>设为默认</button>
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
              <div key={t.id} className="flex-between" style={{ padding: '8px 0', borderBottom: '1px solid #f0f0f0' }}>
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
    </div>
  )
}
