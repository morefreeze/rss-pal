import { findLocalAIPreset, LOCAL_AI_CORS_CHECKED_ON, LOCAL_AI_PRESETS } from '../lib/localAIPresets'
import { useEffect, useRef, useState } from 'react'
import { callLocalAI, clearLocalAIConfig, localAIAccount, readLocalAIConfig, resolveLocalAIConfig, saveLocalAIConfig } from '../lib/localAI'

export default function LocalAISettings() {
  const [saved] = useState(readLocalAIConfig)
  const [config, setConfig] = useState({ api_key: '', base_url: saved?.base_url || '', model: saved?.model || '' })
  const [provider, setProvider] = useState(() => {
    const preset = findLocalAIPreset(saved?.base_url || '')
    return preset?.cors === 'verified' ? preset.id : 'custom'
  })
  const [configured, setConfigured] = useState(!!saved)
  const [remember, setRemember] = useState(!!saved?.remember)
  const [error, setError] = useState('')
  const [status, setStatus] = useState('')
  const [testing, setTesting] = useState(false)
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  const change = (field: keyof typeof config, value: string) => {
    controller.current?.abort()
    setTesting(false); setStatus(''); setError('')
    setConfig(prev => ({ ...prev, [field]: value }))
  }
  const selectProvider = (id: string) => {
    if (id === 'custom') { setProvider(id); return }
    const preset = LOCAL_AI_PRESETS.find(p => p.id === id && p.cors === 'verified')
    if (!preset) return
    controller.current?.abort(); setTesting(false); setError(''); setStatus('')
    setProvider(id)
    setConfig(prev => ({ ...prev, base_url: preset.baseURL, api_key: prev.base_url.replace(/\/+$/, '') === preset.baseURL ? prev.api_key : '' }))
  }
  const save = () => {
    setError(''); setStatus('')
    try {
      saveLocalAIConfig(config, remember)
      setConfigured(true); setConfig(prev => ({ ...prev, api_key: '' }))
      setStatus('已保存到本机。请到文章页手动点击“生成总结”，保存配置不会自动调用 AI。')
    } catch (e) { setError(e instanceof Error ? e.message : '浏览器不允许保存，请检查存储权限') }
  }
  const test = async () => {
    setError(''); setStatus('')
    const ctrl = new AbortController(); controller.current = ctrl
    setTesting(true)
    try {
      const account = localAIAccount()
      await callLocalAI(resolveLocalAIConfig(config), '请只回复 OK。', ctrl.signal, true)
      if (!ctrl.signal.aborted && account === localAIAccount()) setStatus('连通性测试通过：当前配置可由浏览器直连。测试不会保存配置或自动生成文章摘要。')
    } catch (e) {
      if (!ctrl.signal.aborted) setError(e instanceof Error ? e.message : '连通性测试失败')
    } finally {
      if (controller.current === ctrl) { setTesting(false); controller.current = null }
    }
  }
  const clear = () => {
    controller.current?.abort(); setTesting(false)
    try {
      clearLocalAIConfig(); setProvider('custom'); setConfig({ api_key: '', base_url: '', model: '' }); setConfigured(false)
      setError(''); setStatus('已删除此账号在本机的 AI 配置。供应商侧的 Key 仍有效，如需撤销请前往供应商控制台。')
    } catch { setError('本机配置删除失败，请检查浏览器存储权限') }
  }
  return <div className="card mb-2">
    <h3 className="mb-1">我的 AI 配置 · 仅本机</h3>
    <p className="text-muted text-sm mb-1">API Key 仅保存在当前浏览器，不上传 RSS Pal、不跨设备同步。生成文章总结时由浏览器直接发送给你填写的 AI 服务，网站只保存生成后的摘要结果。</p>
    <p className="text-sm mb-1"><strong>需要手动点击生成：</strong>保存配置后不会自动处理文章。请到文章页点击“生成总结”或“重新生成”，并保持页面打开。本机模式目前总结正文与字幕，不发送图片。</p>
    <p className="text-muted text-sm mb-2">原来保存在服务器的用户 Key 已停用并清除，请在此重新填写。本站提供的系统 AI 独立运行，不会读取本机 Key。</p>
    <div className="mb-1">
      <label className="text-sm text-bold" htmlFor="local-ai-provider">AI 服务</label>
      <select id="local-ai-provider" value={provider} onChange={e => selectProvider(e.target.value)} style={{ width: '100%', marginTop: 4 }}>
        <option value="custom">自定义地址</option>
        <optgroup label="已通过浏览器跨域检查">
          {LOCAL_AI_PRESETS.filter(p => p.cors === 'verified').map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </optgroup>
        <optgroup label="官方地址跨域受限 · 暂不可用">
          {LOCAL_AI_PRESETS.filter(p => p.cors === 'blocked').map(p => <option key={p.id} value={p.id} disabled>{p.name}</option>)}
        </optgroup>
      </select>
      <p className="text-muted text-sm">预设地址已于 {LOCAL_AI_CORS_CHECKED_ON} 通过本站浏览器跨域检查，仍需用你的 Key 测试连通性。切换服务商后请重新填写对应的 Key 并确认模型名称；不会自动保存或调用。</p>
      <p className="text-muted text-sm">GLM 官方国内、国际及 Coding 地址尚未通过跨域检查，暂不可选。可选支持 GLM 模型的硅基流动或 OpenRouter，但需使用对应平台的 Key，不能沿用智谱 Key。</p>
    </div>
    <div className="mb-1">
      <label className="text-sm text-bold" htmlFor="local-ai-key">API Key {configured && <span style={{ color: '#16a34a' }}>✓ 本机已配置</span>}</label>
      <input id="local-ai-key" type="password" autoComplete="off" spellCheck={false} value={config.api_key} placeholder={configured ? '留空保留本机 Key；更换地址需重新输入' : 'sk-...'} onChange={e => change('api_key', e.target.value)} style={{ width: '100%', marginTop: 4 }} />
    </div>
    <div className="mb-1">
      <label className="text-sm text-bold" htmlFor="local-ai-url">Base URL</label>
      <input id="local-ai-url" type="url" readOnly={provider !== 'custom'} value={config.base_url} placeholder="https://api.openai.com/v1" onChange={e => change('base_url', e.target.value)} style={{ width: '100%', marginTop: 4 }} />
    </div>
    <p className="text-muted text-sm mb-1">Claude 官方地址使用原生 Messages API；其余地址使用 OpenAI 兼容接口。百炼 Key 需与所选地域匹配。</p>
    <div className="mb-1">
      <label className="text-sm text-bold" htmlFor="local-ai-model">Model</label>
      <input id="local-ai-model" value={config.model} placeholder="供应商提供的模型名称" onChange={e => change('model', e.target.value)} style={{ width: '100%', marginTop: 4 }} />
    </div>
    <label className="text-sm" style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 8 }}>
      <input type="checkbox" style={{ width: 'auto', flexShrink: 0, margin: 0 }} checked={remember} onChange={e => setRemember(e.target.checked)} />在此设备记住 Key（仅限可信的私人设备）
    </label>
    <p className="text-muted text-sm mb-1">不勾选时仅保存于当前标签页会话；勾选后保留到主动清除。浏览器扩展或网站脚本漏洞仍可能读取本地 Key，请使用专用、限额的 Key。</p>
    <p className="text-muted text-sm mb-2">供应商须支持浏览器跨域请求（CORS）；不支持时无法直连。测试连通性会发送一次少量 token 的真实请求，可能产生少量费用。</p>
    {error && <div role="alert" className="text-sm mb-1" style={{ color: '#dc2626' }}>{error}</div>}
    {status && <div role="status" className="text-sm mb-1" style={{ color: '#16a34a' }}>{status}</div>}
    <div className="flex gap-2" style={{ flexWrap: 'wrap' }}>
      <button onClick={save} disabled={testing}>保存到本机</button>
      <button className="secondary" onClick={test} disabled={testing}>{testing ? '测试中...' : '测试连通性'}</button>
      <button className="secondary" onClick={clear}>清除本机配置</button>
    </div>
  </div>
}
