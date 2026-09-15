export function authErrorMessage(error: unknown, fallback: string): string {
  const response = (error as {response?: {status?: number; headers?: Record<string, unknown>; data?: {error?: string}}})?.response
  if (response?.status === 429) {
    const seconds = Number(response.headers?.['retry-after'])
    return Number.isFinite(seconds) && seconds > 0
      ? `操作过于频繁，请在 ${Math.ceil(seconds)} 秒后重试`
      : '操作过于频繁，请稍后重试'
  }
  if (response?.status === 503) return response.data?.error || '认证服务暂时不可用，请稍后重试'
  return response?.data?.error || fallback
}
