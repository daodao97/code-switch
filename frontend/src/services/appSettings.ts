import { Call } from '@wailsio/runtime'

export type AppSettings = {
  show_heatmap: boolean
  show_home_title: boolean
  budget_total: number
  budget_used_adjustment: number
  budget_cycle_enabled: boolean
  budget_cycle_mode: string
  budget_refresh_time: string
  budget_refresh_day: number
  budget_show_countdown: boolean
  budget_show_forecast: boolean
  budget_forecast_method: string
  budget_total_codex: number
  budget_used_adjustment_codex: number
  budget_cycle_enabled_codex: boolean
  budget_cycle_mode_codex: string
  budget_refresh_time_codex: string
  budget_refresh_day_codex: number
  budget_show_countdown_codex: boolean
  budget_show_forecast_codex: boolean
  budget_forecast_method_codex: string
  auto_start: boolean
  auto_update: boolean
  auto_connectivity_test: boolean
  enable_switch_notify: boolean // 供应商切换通知开关
  enable_round_robin: boolean   // 同 Level 轮询负载均衡开关
}

const DEFAULT_SETTINGS: AppSettings = {
  show_heatmap: true,
  show_home_title: true,
  budget_total: 0,
  budget_used_adjustment: 0,
  budget_cycle_enabled: false,
  budget_cycle_mode: 'daily',
  budget_refresh_time: '00:00',
  budget_refresh_day: 1,
  budget_show_countdown: false,
  budget_show_forecast: false,
  budget_forecast_method: 'cycle',
  budget_total_codex: 0,
  budget_used_adjustment_codex: 0,
  budget_cycle_enabled_codex: false,
  budget_cycle_mode_codex: 'daily',
  budget_refresh_time_codex: '00:00',
  budget_refresh_day_codex: 1,
  budget_show_countdown_codex: false,
  budget_show_forecast_codex: false,
  budget_forecast_method_codex: 'cycle',
  auto_start: false,
  auto_update: true,
  auto_connectivity_test: false,
  enable_switch_notify: true,  // 默认开启
  enable_round_robin: false,   // 默认关闭轮询
}

export const fetchAppSettings = async (): Promise<AppSettings> => {
  const data = await Call.ByName('codeswitch/services.AppSettingsService.GetAppSettings')
  return data ?? DEFAULT_SETTINGS
}

export const saveAppSettings = async (settings: AppSettings): Promise<AppSettings> => {
  return Call.ByName('codeswitch/services.AppSettingsService.SaveAppSettings', settings)
}
