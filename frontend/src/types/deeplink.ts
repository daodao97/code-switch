// 本地 DeepLink 类型定义，兼容后端返回的可能 null 字段

export interface DeepLinkImportRequest {
  version: string
  resource: string
  app: string
  name: string
  homepage: string
  endpoint: string
  apiKey: string
  model?: string | null
  notes?: string | null
  haikuModel?: string | null
  sonnetModel?: string | null
  opusModel?: string | null
  config?: string | null
  configFormat?: string | null
  configUrl?: string | null
}
