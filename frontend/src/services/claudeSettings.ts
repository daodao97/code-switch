import { Call } from '@wailsio/runtime'

// 本地类型定义，避免依赖 CI 生成的绑定文件
export interface ClaudeProxyStatus {
  enabled: boolean
  base_url: string
}

type Platform = 'claude' | 'codex'

const serviceNames: Record<Platform, string> = {
  claude: 'codeswitch/services.ClaudeSettingsService',
  codex: 'codeswitch/services.CodexSettingsService',
}

const callByPlatform = async <T = unknown>(platform: Platform, method: string, payload?: any[]): Promise<T> => {
  const service = serviceNames[platform]
  const args = payload ?? []
  return Call.ByName(`${service}.${method}`, ...args)
}

// 归一化代理状态字段（兼容 Wails 返回的 Go 导出字段名 Enabled/BaseURL）
const normalizeProxyStatus = (raw: any): ClaudeProxyStatus => ({
  enabled: Boolean(raw?.enabled ?? raw?.Enabled),
  base_url: raw?.base_url ?? raw?.BaseURL ?? '',
})

export const fetchProxyStatus = async (platform: Platform): Promise<ClaudeProxyStatus> => {
  const raw = await callByPlatform(platform, 'ProxyStatus')
  return normalizeProxyStatus(raw)
}

export const enableProxy = async (platform: Platform): Promise<void> => {
  await callByPlatform(platform, 'EnableProxy')
}

export const disableProxy = async (platform: Platform): Promise<void> => {
  await callByPlatform(platform, 'DisableProxy')
}
