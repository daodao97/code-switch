import { Call } from '@wailsio/runtime'

export type AppSettings = {
  show_heatmap: boolean
  show_home_title: boolean
  auto_start: boolean
  auto_update: boolean
  auto_connectivity_test: boolean
  enable_switch_notify: boolean // 供应商切换通知开关
  enable_round_robin: boolean   // 同 Level 轮询负载均衡开关
}

const DEFAULT_SETTINGS: AppSettings = {
  show_heatmap: true,
  show_home_title: true,
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
