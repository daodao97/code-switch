/**
 * 从未知类型中尽量提取可读错误信息
 * 兼容 Wails 可能返回的错误对象，避免显示 [object Object]
 */
export function extractErrorMessage(err: unknown, fallback = '未知错误'): string {
  if (err == null) return fallback

  if (typeof err === 'string') return err

  if (err instanceof Error) {
    return err.message || fallback
  }

  if (err && typeof err === 'object') {
    const obj: any = err
    // 尝试提取常见的错误信息字段
    const msg = obj.message ?? obj.Message
    if (typeof msg === 'string' && msg.trim()) {
      return msg
    }

    // 兜底：JSON 序列化，避免 [object Object]
    try {
      return JSON.stringify(err)
    } catch {
      return fallback
    }
  }

  try {
    return String(err)
  } catch {
    return fallback
  }
}
