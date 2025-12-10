export type AutomationCard = {
  id: number
  name: string
  apiUrl: string
  apiKey: string
  officialSite: string
  icon: string
  tint: string
  accent: string
  enabled: boolean
  // 模型白名单：声明 provider 支持的模型（精确或通配符）
  supportedModels?: Record<string, boolean>
  // 模型映射：external model -> internal model
  modelMapping?: Record<string, string>
  // 优先级分组：数字越小优先级越高（1-10，默认 1）
  level?: number
  // CLI 配置：存储供应商关联的 CLI 可编辑配置
  cliConfig?: Record<string, any>

  // === 可用性监控配置（新） ===
  // 可用性监控开关：是否启用后台健康检查
  availabilityMonitorEnabled?: boolean
  // 连通性自动拉黑：检测失败时是否自动拉黑该供应商
  connectivityAutoBlacklist?: boolean
  // 可用性高级配置：测试模型、端点和超时
  availabilityConfig?: {
    testModel?: string      // 测试用模型
    testEndpoint?: string   // 测试端点路径
    timeout?: number        // 超时时间（毫秒）
  }

  // === 旧连通性字段（已废弃，仅用于兼容旧数据） ===
  /** @deprecated 已迁移到 availabilityMonitorEnabled */
  connectivityCheck?: boolean
  /** @deprecated 已迁移到 availabilityConfig.testModel */
  connectivityTestModel?: string
  /** @deprecated 已迁移到 availabilityConfig.testEndpoint */
  connectivityTestEndpoint?: string
  /** @deprecated 已迁移到可用性配置中的认证方式 */
  connectivityAuthType?: string
}

export const automationCardGroups: Record<'claude' | 'codex', AutomationCard[]> = {
  claude: [
    {
      id: 100,
      name: '0011',
      apiUrl: 'https://0011.ai',
      apiKey: '',
      officialSite: 'https://0011.ai',
      icon: 'aicoding',
      tint: 'rgba(10, 132, 255, 0.14)',
      accent: '#0aff5cff',
      enabled: false,
    },
    {
      id: 101,
      name: 'AICoding.sh',
      apiUrl: 'https://api.aicoding.sh',
      apiKey: '',
      officialSite: 'https://aicoding.sh',
      icon: 'aicoding',
      tint: 'rgba(10, 132, 255, 0.14)',
      accent: '#0a84ff',
      enabled: false,
    },
    {
      id: 102,
      name: 'Kimi',
      apiUrl: 'https://api.moonshot.cn/anthropic',
      apiKey: '',
      officialSite: 'https://kimi.moonshot.cn',
      icon: 'kimi',
      tint: 'rgba(16, 185, 129, 0.16)',
      accent: '#10b981',
      enabled: false,
    },
    {
      id: 103,
      name: 'Deepseek',
      apiUrl: 'https://api.deepseek.com/anthropic',
      apiKey: '',
      officialSite: 'https://www.deepseek.com',
      icon: 'deepseek',
      tint: 'rgba(251, 146, 60, 0.18)',
      accent: '#f97316',
      enabled: false,
    },
  ],
  codex: [
    {
      id: 201,
      name: 'AICoding.sh',
      apiUrl: 'https://api.aicoding.sh',
      apiKey: '',
      officialSite: 'https://www.aicoding.sh',
      icon: 'aicoding',
      tint: 'rgba(236, 72, 153, 0.16)',
      accent: '#ec4899',
      enabled: false,
    },
  ],
}

export function createAutomationCards(data: AutomationCard[] = []): AutomationCard[] {
  return data.map((item) => ({
    ...item,
    officialSite: item.officialSite ?? '',
  }))
}
