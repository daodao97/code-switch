import { Call } from '@wailsio/runtime'

// 本地类型定义，避免依赖 CI 生成的绑定文件
export interface GeminiProxyStatus {
  enabled: boolean
  base_url: string
}

const serviceName = 'codeswitch/services.GeminiService'

// 归一化代理状态字段（兼容 Wails 返回的 Go 导出字段名 Enabled/BaseURL）
const normalizeProxyStatus = (raw: any): GeminiProxyStatus => ({
  enabled: Boolean(raw?.enabled ?? raw?.Enabled),
  base_url: raw?.base_url ?? raw?.BaseURL ?? '',
})

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
