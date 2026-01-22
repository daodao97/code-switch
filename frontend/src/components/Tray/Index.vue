<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import { Call } from '@wailsio/runtime'
import { fetchCostSince, fetchLogStats } from '../../services/logs'
import { fetchAppSettings } from '../../services/appSettings'

const rootRef = ref<HTMLElement | null>(null)
const used = ref(0)
const total = ref(0)
const loading = ref(false)
const cycleEnabled = ref(false)
const cycleMode = ref<'daily' | 'weekly'>('daily')
const refreshTime = ref('00:00')
const refreshDay = ref(1)
const showCountdown = ref(false)
const showForecast = ref(false)
const countdownLabel = ref('')
const forecastLabel = ref('')
let ticker: number | undefined
let cycleStart: Date | null = null
let nextReset: Date | null = null

const formatCurrency = (value?: number) => {
  if (value === undefined || value === null || Number.isNaN(value)) {
    return '$0.0000'
  }
  if (value >= 1) {
    return `$${value.toFixed(2)}`
  }
  if (value >= 0.01) {
    return `$${value.toFixed(3)}`
  }
  return `$${value.toFixed(4)}`
}

const usedLabel = computed(() => formatCurrency(used.value))
const totalLabel = computed(() => (total.value > 0 ? formatCurrency(total.value) : '未设置'))
const progressRatio = computed(() => {
  if (total.value <= 0) return 0
  return Math.min(Math.max(used.value / total.value, 0), 1)
})
const progressPercentLabel = computed(() => {
  const percent = Math.round(progressRatio.value * 100)
  return `${percent}%`
})
const budgetTitle = computed(() => (cycleEnabled.value && cycleMode.value === 'weekly' ? '本周预算' : '今日预算'))

const pad2 = (value: number) => String(value).padStart(2, '0')

const formatLocalDateTime = (date: Date) => {
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}:${pad2(date.getSeconds())}`
}

const startOfDay = (date: Date) => {
  const base = new Date(date)
  base.setHours(0, 0, 0, 0)
  return base
}

const parseRefreshTime = () => {
  const [rawHour, rawMinute] = refreshTime.value.split(':')
  const hour = Number(rawHour)
  const minute = Number(rawMinute)
  return {
    hour: Number.isFinite(hour) ? Math.min(Math.max(hour, 0), 23) : 0,
    minute: Number.isFinite(minute) ? Math.min(Math.max(minute, 0), 59) : 0,
  }
}

const normalizeRefreshDay = () => {
  const value = Number(refreshDay.value)
  if (!Number.isFinite(value)) return 1
  return Math.min(Math.max(Math.floor(value), 0), 6)
}

const updateCycleTimes = () => {
  const now = new Date()
  if (!cycleEnabled.value) {
    cycleStart = startOfDay(now)
    nextReset = null
    return
  }
  const { hour, minute } = parseRefreshTime()
  if (cycleMode.value === 'weekly') {
    const desiredDay = normalizeRefreshDay()
    const target = new Date(now)
    const currentDay = target.getDay()
    const diff = desiredDay - currentDay
    target.setDate(target.getDate() + diff)
    target.setHours(hour, minute, 0, 0)
    if (now < target) {
      const start = new Date(target)
      start.setDate(start.getDate() - 7)
      cycleStart = start
      nextReset = target
      return
    }
    cycleStart = target
    const next = new Date(target)
    next.setDate(target.getDate() + 7)
    nextReset = next
    return
  }
  const start = new Date(now)
  start.setHours(hour, minute, 0, 0)
  if (now < start) {
    start.setDate(start.getDate() - 1)
  }
  const next = new Date(start)
  next.setDate(start.getDate() + 1)
  cycleStart = start
  nextReset = next
}

const formatCountdown = (remainingMs: number) => {
  const totalSeconds = Math.max(Math.floor(remainingMs / 1000), 0)
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  return `${pad2(days)}天 ${pad2(hours)}:${pad2(minutes)}:${pad2(seconds)}`
}

const updateDerivedLabels = () => {
  const now = new Date()
  if (showCountdown.value && cycleEnabled.value && nextReset) {
    const remaining = nextReset.getTime() - now.getTime()
    countdownLabel.value = remaining > 0 ? `重置倒计时 ${formatCountdown(remaining)}` : '即将重置'
  } else {
    countdownLabel.value = ''
  }

  if (cycleEnabled.value && nextReset && now >= nextReset && !loading.value) {
    void refresh()
    return
  }

  if (showForecast.value && total.value > 0) {
    const start = cycleStart ?? startOfDay(now)
    const elapsedSeconds = Math.max((now.getTime() - start.getTime()) / 1000, 1)
    const rate = used.value / elapsedSeconds
    if (rate > 0 && used.value < total.value) {
      const secondsToBudget = (total.value - used.value) / rate
      const forecastTime = new Date(now.getTime() + secondsToBudget * 1000)
      forecastLabel.value = `预计耗尽 ${formatLocalDateTime(forecastTime)}`
    } else if (used.value >= total.value && total.value > 0) {
      forecastLabel.value = '已达预算'
    } else {
      forecastLabel.value = '预计耗尽 —'
    }
  } else {
    forecastLabel.value = ''
  }
}

const setupTicker = () => {
  if (ticker) {
    window.clearInterval(ticker)
  }
  if (showCountdown.value || showForecast.value) {
    ticker = window.setInterval(updateDerivedLabels, 1000)
  }
}

const resizeToContent = async () => {
  await nextTick()
  if (!rootRef.value) return
  const height = Math.ceil(rootRef.value.getBoundingClientRect().height)
  if (height <= 0) return
  try {
    await Call.ByName('main.AppService.SetTrayWindowHeight', height)
  } catch (error) {
    console.error('failed to resize tray window', error)
  }
}

const refresh = async () => {
  loading.value = true
  try {
    const settings = await fetchAppSettings()
    total.value = Number(settings?.budget_total ?? 0)
    cycleEnabled.value = settings?.budget_cycle_enabled ?? false
    cycleMode.value = settings?.budget_cycle_mode === 'weekly' ? 'weekly' : 'daily'
    refreshTime.value = settings?.budget_refresh_time || '00:00'
    refreshDay.value = Number.isFinite(settings?.budget_refresh_day)
      ? settings?.budget_refresh_day
      : 1
    showCountdown.value = settings?.budget_show_countdown ?? false
    showForecast.value = settings?.budget_show_forecast ?? false
    updateCycleTimes()

    if (cycleEnabled.value && cycleStart) {
      const startValue = formatLocalDateTime(cycleStart)
      used.value = Number(await fetchCostSince(startValue, ''))
    } else {
      const stats = await fetchLogStats('')
      used.value = Number(stats?.cost_total ?? 0)
    }
  } catch (error) {
    console.error('failed to load tray stats', error)
  } finally {
    loading.value = false
    updateDerivedLabels()
    setupTicker()
    await resizeToContent()
  }
}

const handleFocus = () => {
  void refresh()
}

onMounted(() => {
  void refresh()
  window.addEventListener('focus', handleFocus)
  window.addEventListener('app-settings-updated', handleFocus)
})

onUnmounted(() => {
  if (ticker) {
    window.clearInterval(ticker)
  }
  window.removeEventListener('focus', handleFocus)
  window.removeEventListener('app-settings-updated', handleFocus)
})
</script>

<template>
  <div ref="rootRef" class="tray-root">
    <div class="tray-panel">
      <div class="tray-item">
        <div class="tray-item__header">
          <div class="tray-item__title">
            <span class="tray-dot"></span>
            <span>{{ budgetTitle }}</span>
          </div>
          <div class="tray-item__summary">
            <div class="tray-item__value" :class="{ loading }">
              <span>已用 {{ usedLabel }}</span>
              <span class="tray-divider">/</span>
              <span>{{ totalLabel }}</span>
            </div>
            <span class="tray-item__percent">{{ progressPercentLabel }}</span>
          </div>
        </div>
        <div class="tray-progress">
          <div class="tray-progress__bar" :style="{ width: `${progressRatio * 100}%` }"></div>
        </div>
        <div v-if="countdownLabel || forecastLabel" class="tray-meta">
          <span v-if="countdownLabel">{{ countdownLabel }}</span>
          <span v-if="forecastLabel">{{ forecastLabel }}</span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.tray-root {
  padding: 10px;
}

.tray-panel {
  background: #f1f2f4;
  border-radius: 16px;
  padding: 12px 14px;
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.18);
  border: 1px solid rgba(0, 0, 0, 0.05);
}

.tray-item {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.tray-item__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.tray-item__summary {
  display: flex;
  align-items: center;
  gap: 10px;
}

.tray-item__title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #2f2f2f;
}

.tray-dot {
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: #5dbb63;
  box-shadow: 0 0 0 2px rgba(93, 187, 99, 0.2);
}

.tray-item__value {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #5b5f66;
}

.tray-item__value.loading {
  opacity: 0.6;
}

.tray-divider {
  opacity: 0.5;
}

.tray-item__percent {
  font-size: 12px;
  font-weight: 600;
  color: #5dbb63;
  min-width: 36px;
  text-align: right;
}

.tray-progress {
  width: 100%;
  height: 8px;
  border-radius: 999px;
  background: #e1e4e8;
  overflow: hidden;
}

.tray-progress__bar {
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #5dbb63 0%, #6bd36f 100%);
  transition: width 0.2s ease;
}

.tray-meta {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
  color: #7a7f86;
}

:global(.dark) .tray-panel {
  background: #2c2f35;
  border-color: rgba(255, 255, 255, 0.06);
  box-shadow: 0 12px 26px rgba(0, 0, 0, 0.4);
}

:global(.dark) .tray-item__title {
  color: #f1f5f9;
}

:global(.dark) .tray-item__value {
  color: rgba(241, 245, 249, 0.7);
}

:global(.dark) .tray-item__percent {
  color: #7ce07f;
}

:global(.dark) .tray-progress {
  background: rgba(255, 255, 255, 0.12);
}

:global(.dark) .tray-progress__bar {
  background: linear-gradient(90deg, #5dbb63 0%, #7ce07f 100%);
}

:global(.dark) .tray-meta {
  color: rgba(241, 245, 249, 0.6);
}
</style>
