import { Call } from '@wailsio/runtime'

export type McpPlatform = 'claude-code' | 'codex'
export type McpServerType = 'stdio' | 'http'

export type McpServer = {
  name: string
  type: McpServerType
  command?: string
  args: string[]
  env: Record<string, string>
  url?: string
  website?: string
  tips?: string
  enable_platform: McpPlatform[]
  enabled_in_claude: boolean
  enabled_in_codex: boolean
  missing_placeholders: string[]
}

export const fetchMcpServers = async (): Promise<McpServer[]> => {
  const response = await Call.ByName('codeswitch/services.MCPService.ListServers')
  return (response as McpServer[]) ?? []
}

export const saveMcpServers = async (servers: McpServer[]): Promise<void> => {
  await Call.ByName('codeswitch/services.MCPService.SaveServers', servers)
}

// JSON 导入相关类型
export type MCPParseResult = {
  servers: McpServer[]
  conflicts: string[]
  needName: boolean
}

export type ConflictStrategy = 'overwrite' | 'skip' | 'rename'

// 解析 JSON 字符串为 MCP 服务器列表
export const parseMCPJSON = async (jsonStr: string): Promise<MCPParseResult> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ParseMCPJSON', jsonStr)
  return response as MCPParseResult
}

// 批量导入 MCP 服务器
export const importMCPFromJSON = async (servers: McpServer[], conflictStrategy: ConflictStrategy): Promise<number> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ImportMCPFromJSON', servers, conflictStrategy)
  return response as number
}
