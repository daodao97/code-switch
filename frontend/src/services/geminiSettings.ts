import { Call } from '@wailsio/runtime'

// 本地类型定义，避免依赖 CI 生成的绑定文件
export interface GeminiProxyStatus {
  enabled: boolean
  base_url: string
}

const serviceName = 'codeswitch/services.GeminiService'

// 归一化代理状态字段（兼容 Wails 返回的 Go 导出字段名 Enabled/BaseURL）
// 注意：Wails 绑定会给字段赋默认值，所以用 'in' 检查而非 ??
const normalizeProxyStatus = (raw: any): GeminiProxyStatus => {
  const obj = raw ?? {}
  const enabled = 'Enabled' in obj ? obj.Enabled : obj.enabled
  const baseURL = 'BaseURL' in obj ? obj.BaseURL : obj.base_url
  return {
    enabled: enabled === undefined ? false : Boolean(enabled),
    base_url: typeof baseURL === 'string' ? baseURL : '',
  }
}

export const fetchGeminiProxyStatus = async (): Promise<GeminiProxyStatus> => {
  const raw = await Call.ByName(`${serviceName}.ProxyStatus`)
  return normalizeProxyStatus(raw)
}

export const enableGeminiProxy = async (): Promise<void> => {
  await Call.ByName(`${serviceName}.EnableProxy`)
}

export const disableGeminiProxy = async (): Promise<void> => {
  await Call.ByName(`${serviceName}.DisableProxy`)
}
