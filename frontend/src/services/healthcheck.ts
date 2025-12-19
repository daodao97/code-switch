// services/healthcheck.ts
// 可用性监控服务前端调用层
// Author: Half open flowers

import { Call } from '@wailsio/runtime'

// 健康状态常量
export const HealthStatus = {
  OPERATIONAL: 'operational',
  DEGRADED: 'degraded',
  FAILED: 'failed',
  VALIDATION_ERROR: 'validation_failed',
} as const

// 健康检查结果类型
export interface HealthCheckResult {
  id: number
  providerId: number
  providerName: string
  platform: string
  model?: string
  endpoint?: string
  status: string
  latencyMs: number
  errorMessage: string
  checkedAt: string
}

// Provider 时间线类型
export interface ProviderTimeline {
  providerId: number
  providerName: string
  platform: string
  availabilityMonitorEnabled: boolean
  connectivityAutoBlacklist: boolean
  availabilityConfig?: AvailabilityConfig | null // 高级配置
  items: HealthCheckResult[]
  latest: HealthCheckResult | null
  uptime: number
  avgLatencyMs: number
}

// 可用性高级配置
export interface AvailabilityConfig {
  testModel?: string
  testEndpoint?: string
  timeout?: number
}

const SERVICE_PATH = 'codeswitch/services.HealthCheckService'

/**
 * 获取所有 Provider 的最新状态（按平台分组）
 */
export async function getLatestResults(): Promise<Record<string, ProviderTimeline[]>> {
  return Call.ByName(`${SERVICE_PATH}.GetLatestResults`)
}

/**
 * 获取单个 Provider 的历史记录
 */
export async function getHistory(platform: string, providerName: string, limit: number = 20): Promise<any> {
  return Call.ByName(`${SERVICE_PATH}.GetHistory`, platform, providerName, limit)
}

/**
 * 手动触发单个 Provider 检测
 */
export async function runSingleCheck(platform: string, providerId: number): Promise<HealthCheckResult> {
  return Call.ByName(`${SERVICE_PATH}.RunSingleCheck`, platform, providerId)
}

/**
 * 手动触发全部检测
 */
export async function runAllChecks(): Promise<Record<string, HealthCheckResult[]>> {
  return Call.ByName(`${SERVICE_PATH}.RunAllChecks`)
}

/**
 * 启动后台定时巡检
 */
export async function startBackgroundPolling(): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.StartBackgroundPolling`)
}

/**
 * 停止后台巡检
 */
export async function stopBackgroundPolling(): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.StopBackgroundPolling`)
}

/**
 * 检查后台巡检是否运行中
 */
export async function isPollingRunning(): Promise<boolean> {
  return Call.ByName(`${SERVICE_PATH}.IsPollingRunning`)
}

/**
 * 启用/禁用指定 Provider 的可用性监控
 */
export async function setAvailabilityMonitorEnabled(
  platform: string,
  providerId: number,
  enabled: boolean
): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.SetAvailabilityMonitorEnabled`, platform, providerId, enabled)
}

/**
 * 启用/禁用指定 Provider 的连通性自动拉黑
 */
export async function setConnectivityAutoBlacklist(
  platform: string,
  providerId: number,
  enabled: boolean
): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.SetConnectivityAutoBlacklist`, platform, providerId, enabled)
}

/**
 * 保存 Provider 的可用性高级配置
 */
export async function saveAvailabilityConfig(
  platform: string,
  providerId: number,
  config: AvailabilityConfig
): Promise<void> {
  return Call.ByName(`${SERVICE_PATH}.SaveAvailabilityConfig`, platform, providerId, config)
}

/**
 * 清理过期的历史记录
 */
export async function cleanupOldRecords(daysToKeep: number = 7): Promise<number> {
  return Call.ByName(`${SERVICE_PATH}.CleanupOldRecords`, daysToKeep)
}

/**
 * 格式化状态为中文
 */
export function formatStatus(status: string): string {
  switch (status) {
    case HealthStatus.OPERATIONAL:
      return '正常'
    case HealthStatus.DEGRADED:
      return '延迟'
    case HealthStatus.FAILED:
      return '故障'
    case HealthStatus.VALIDATION_ERROR:
      return '验证失败'
    default:
      return status
  }
}

/**
 * 获取状态对应的颜色类
 */
export function getStatusColor(status: string): string {
  switch (status) {
    case HealthStatus.OPERATIONAL:
      return 'text-green-500'
    case HealthStatus.DEGRADED:
      return 'text-yellow-500'
    case HealthStatus.FAILED:
      return 'text-red-500'
    case HealthStatus.VALIDATION_ERROR:
      return 'text-red-500'
    default:
      return 'text-gray-500'
  }
}

/**
 * 获取状态对应的图标
 */
export function getStatusIcon(status: string): string {
  switch (status) {
    case HealthStatus.OPERATIONAL:
      return '\u{1F7E2}' // green circle
    case HealthStatus.DEGRADED:
      return '\u{1F7E1}' // yellow circle
    case HealthStatus.FAILED:
      return '\u{1F534}' // red circle
    case HealthStatus.VALIDATION_ERROR:
      return '\u{1F534}' // red circle
    default:
      return '\u{26AB}' // black circle
  }
}
