import { Call } from '@wailsio/runtime'

export type McpPlatform = 'claude-code' | 'codex' | 'gemini'
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
  enabled_in_gemini: boolean
  missing_placeholders: string[]
}

export const fetchMcpServers = async (): Promise<McpServer[]> => {
  const response = await Call.ByName('codeswitch/services.MCPService.ListServers')
  return (response as McpServer[]) ?? []
}

export const saveMcpServers = async (servers: McpServer[]): Promise<void> => {
  await Call.ByName('codeswitch/services.MCPService.SaveServers', servers)
}

export type McpParseResult = {
  servers: McpServer[]
  conflicts: string[]
  needName: boolean
}

export type ConflictStrategy = 'skip' | 'overwrite'

export const parseMcpJSON = async (jsonStr: string): Promise<McpParseResult | null> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ParseMCPJSON', jsonStr)
  return response as McpParseResult | null
}

export const importMcpServers = async (
  servers: McpServer[],
  strategy: ConflictStrategy
): Promise<number> => {
  const response = await Call.ByName('codeswitch/services.ImportService.ImportMCPServers', servers, strategy)
  return (response as number) ?? 0
}
