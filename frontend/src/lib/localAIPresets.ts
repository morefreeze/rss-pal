export interface LocalAIPreset {
  id: string
  name: string
  baseURL: string
  protocol: 'openai' | 'anthropic'
  cors: 'verified' | 'blocked'
}

// Browser probes from https://rss.morefreeze.top, 2026-10-26. Verification
// covers preflight + readable invalid-key responses, not paid inference.
// Evidence: docs/operations/evidence/2026-10-26-personal-ai-cors.json.
export const LOCAL_AI_CORS_CHECKED_ON = '2026-10-26'
export const LOCAL_AI_PRESETS: readonly LocalAIPreset[] = [
  { id: 'claude', name: 'Claude / Anthropic', baseURL: 'https://api.anthropic.com/v1', protocol: 'anthropic', cors: 'verified' },
  { id: 'deepseek', name: 'DeepSeek', baseURL: 'https://api.deepseek.com', protocol: 'openai', cors: 'verified' },
  { id: 'openai', name: 'OpenAI', baseURL: 'https://api.openai.com/v1', protocol: 'openai', cors: 'verified' },
  { id: 'siliconflow', name: '硅基流动', baseURL: 'https://api.siliconflow.cn/v1', protocol: 'openai', cors: 'verified' },
  { id: 'openrouter', name: 'OpenRouter', baseURL: 'https://openrouter.ai/api/v1', protocol: 'openai', cors: 'verified' },
  { id: 'bailian-cn', name: '阿里云百炼 · 北京', baseURL: 'https://dashscope.aliyuncs.com/compatible-mode/v1', protocol: 'openai', cors: 'verified' },
  { id: 'bailian-intl', name: '阿里云百炼 · 新加坡', baseURL: 'https://dashscope-intl.aliyuncs.com/compatible-mode/v1', protocol: 'openai', cors: 'verified' },
  { id: 'moonshot', name: 'Kimi / Moonshot', baseURL: 'https://api.moonshot.cn/v1', protocol: 'openai', cors: 'verified' },
  { id: 'xai', name: 'xAI / Grok', baseURL: 'https://api.x.ai/v1', protocol: 'openai', cors: 'verified' },
  { id: 'groq', name: 'Groq', baseURL: 'https://api.groq.com/openai/v1', protocol: 'openai', cors: 'verified' },
  { id: 'glm-cn', name: 'GLM · 智谱国内按量（跨域受限）', baseURL: 'https://open.bigmodel.cn/api/paas/v4', protocol: 'openai', cors: 'blocked' },
  { id: 'glm-coding-cn', name: 'GLM · 智谱国内 Coding（跨域受限）', baseURL: 'https://open.bigmodel.cn/api/coding/paas/v4', protocol: 'openai', cors: 'blocked' },
  { id: 'zai', name: 'GLM · Z.AI 国际按量（跨域受限）', baseURL: 'https://api.z.ai/api/paas/v4', protocol: 'openai', cors: 'blocked' },
  { id: 'zai-coding', name: 'GLM · Z.AI 国际 Coding（跨域受限）', baseURL: 'https://api.z.ai/api/coding/paas/v4', protocol: 'openai', cors: 'blocked' },
]

export function findLocalAIPreset(baseURL: string): LocalAIPreset | undefined {
  const normalized = baseURL.trim().replace(/\/+$/, '')
  return LOCAL_AI_PRESETS.find(p => p.baseURL === normalized)
}
