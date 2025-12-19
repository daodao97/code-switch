import { Call } from '@wailsio/runtime'

// CLI 平台类型
export type CLIPlatform = 'claude' | 'codex' | 'gemini'

// 配置字段信息
export interface CLIConfigField {
  key: string
  value: string
  locked: boolean
  hint?: string
  type: 'string' | 'boolean' | 'object'
  required?: boolean
}

// 配置文件预览（用于前端显示原始内容）
export interface CLIConfigFile {
  path: string
  format?: 'json' | 'toml' | 'env' | string
  content: string
}

// CLI 配置数据
export interface CLIConfig {
  platform: CLIPlatform
  fields: CLIConfigField[]
  rawContent?: string
  rawFiles?: CLIConfigFile[]
  configFormat?: 'json' | 'toml' | 'env'
  envContent?: Record<string, string>
  filePath?: string
  editable?: Record<string, any>
}

// CLI 配置模板
export interface CLITemplate {
  template: Record<string, any>
  isGlobalDefault: boolean
}

// CLI 配置快照（用于前端对比：当前 vs 预览）
export interface CLIConfigSnapshots {
  currentFiles: CLIConfigFile[]
  previewFiles: CLIConfigFile[]
  mode: 'proxy' | 'direct'
}

const SERVICE_PATH = 'codeswitch/services.CliConfigService'

// 获取指定平台的 CLI 配置
export async function fetchCLIConfig(platform: CLIPlatform): Promise<CLIConfig> {
  return Call.ByName(`${SERVICE_PATH}.GetConfig`, platform)
}

// 保存 CLI 配置
export async function saveCLIConfig(platform: CLIPlatform, editable: Record<string, any>): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.SaveConfig`, platform, editable)
}

// 保存指定配置文件内容（预览区高级编辑）
export async function saveCLIConfigFileContent(
  platform: CLIPlatform,
  filePath: string,
  content: string
): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.SaveConfigFileContent`, platform, filePath, content)
}

// 获取指定平台的全局模板
export async function fetchCLITemplate(platform: CLIPlatform): Promise<CLITemplate> {
  return Call.ByName(`${SERVICE_PATH}.GetTemplate`, platform)
}

// 设置指定平台的全局模板
export async function setCLITemplate(
  platform: CLIPlatform,
  template: Record<string, any>,
  isGlobalDefault: boolean
): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.SetTemplate`, platform, template, isGlobalDefault)
}

// 获取指定平台的锁定字段列表
export async function fetchLockedFields(platform: CLIPlatform): Promise<string[]> {
  return Call.ByName(`${SERVICE_PATH}.GetLockedFields`, platform)
}

// 恢复默认配置
export async function restoreDefaultConfig(platform: CLIPlatform): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.RestoreDefault`, platform)
}

// 获取配置快照（用于 Preview/Current 对比）
// 预览规则：
//   - 若传入 apiUrl/apiKey 任一非空：模拟 ApplySingleProvider() 的写入结果（直连模式）
//   - 若二者都为空：模拟 EnableProxy() 的写入结果（代理模式）
export async function fetchCLIConfigSnapshots(
  platform: CLIPlatform,
  apiUrl: string = '',
  apiKey: string = ''
): Promise<CLIConfigSnapshots> {
  return Call.ByName(`${SERVICE_PATH}.GetConfigSnapshots`, platform, apiUrl, apiKey)
}
