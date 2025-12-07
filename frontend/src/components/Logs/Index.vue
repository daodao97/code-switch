<template>
  <div class="logs-page">
    <div class="logs-header">
      <BaseButton variant="outline" type="button" @click="backToHome">
        {{ t('components.logs.back') }}
      </BaseButton>
      <div class="refresh-indicator">
        <span>{{ t('components.logs.nextRefresh', { seconds: countdown }) }}</span>
        <BaseButton size="sm" :disabled="loading" @click="manualRefresh">
          {{ t('components.logs.refresh') }}
        </BaseButton>
      </div>
    </div>

    <section class="logs-summary" v-if="statsCards.length">
      <article
        v-for="card in statsCards"
        :key="card.key"
        :class="['summary-card', { 'summary-card--clickable': card.key === 'cost' }]"
        @click="card.key === 'cost' && openCostDetailModal()"
      >
        <div class="summary-card__label">{{ card.label }}</div>
        <div class="summary-card__value">{{ card.value }}</div>
        <div class="summary-card__hint">{{ card.hint }}</div>
      </article>
    </section>

    <section class="logs-chart">
      <Line :data="chartData" :options="chartOptions" />
    </section>

    <form class="logs-filter-row" @submit.prevent="applyFilters">
      <div class="filter-fields">
        <label class="filter-field">
          <span>{{ t('components.logs.filters.platform') }}</span>
          <select v-model="filters.platform" class="mac-select">
            <option value="">{{ t('components.logs.filters.allPlatforms') }}</option>
            <option value="claude">Claude</option>
            <option value="codex">Codex</option>
            <option value="gemini">Gemini</option>
          </select>
        </label>
        <label class="filter-field">
          <span>{{ t('components.logs.filters.provider') }}</span>
          <select v-model="filters.provider" class="mac-select">
            <option value="">{{ t('components.logs.filters.allProviders') }}</option>
            <option v-for="provider in providerOptions" :key="provider" :value="provider">
              {{ provider }}
            </option>
          </select>
        </label>
      </div>
      <div class="filter-actions">
        <BaseButton type="submit" :disabled="loading">
          {{ t('components.logs.query') }}
        </BaseButton>
      </div>
    </form>

    <section class="logs-table-wrapper">
      <table class="logs-table">
        <thead>
          <tr>
            <th class="col-time">{{ t('components.logs.table.time') }}</th>
            <th class="col-platform">{{ t('components.logs.table.platform') }}</th>
            <th class="col-provider">{{ t('components.logs.table.provider') }}</th>
            <th class="col-model">{{ t('components.logs.table.model') }}</th>
            <th class="col-http">{{ t('components.logs.table.httpCode') }}</th>
            <th class="col-stream">{{ t('components.logs.table.stream') }}</th>
            <th class="col-duration">{{ t('components.logs.table.duration') }}</th>
            <th class="col-tokens">{{ t('components.logs.table.tokens') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in pagedLogs" :key="item.id">
            <td>{{ formatTime(item.created_at) }}</td>
            <td>{{ item.platform || '—' }}</td>
            <td>{{ item.provider || '—' }}</td>
            <td>{{ item.model || '—' }}</td>
            <td :class="['code', httpCodeClass(item.http_code)]">{{ item.http_code }}</td>
            <td><span :class="['stream-tag', item.is_stream ? 'on' : 'off']">{{ formatStream(item.is_stream) }}</span></td>
            <td><span :class="['duration-tag', durationColor(item.duration_sec)]">{{ formatDuration(item.duration_sec) }}</span></td>
            <td class="token-cell">
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.input') }}</span>
                <span class="token-value">{{ formatNumber(item.input_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.output') }}</span>
                <span class="token-value">{{ formatNumber(item.output_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.reasoning') }}</span>
                <span class="token-value">{{ formatNumber(item.reasoning_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.cacheWrite') }}</span>
                <span class="token-value">{{ formatNumber(item.cache_create_tokens) }}</span>
              </div>
              <div>
                <span class="token-label">{{ t('components.logs.tokenLabels.cacheRead') }}</span>
                <span class="token-value">{{ formatNumber(item.cache_read_tokens) }}</span>
              </div>
            </td>
          </tr>
          <tr v-if="!pagedLogs.length && !loading">
            <td colspan="8" class="empty">{{ t('components.logs.empty') }}</td>
          </tr>
        </tbody>
      </table>
      <p v-if="loading" class="empty">{{ t('components.logs.loading') }}</p>
    </section>

    <div class="logs-pagination">
      <span>{{ page }} / {{ totalPages }}</span>
      <div class="pagination-actions">
        <BaseButton variant="outline" size="sm" :disabled="page === 1 || loading" @click="prevPage">
          ‹
        </BaseButton>
        <BaseButton variant="outline" size="sm" :disabled="page >= totalPages || loading" @click="nextPage">
          ›
        </BaseButton>
      </div>
    </div>

    <!-- 金额明细弹窗 -->
    <BaseModal
      :open="costDetailModal.open"
      :title="t('components.logs.costDetail.title')"
      @close="closeCostDetailModal"
    >
      <div class="cost-detail-modal">
        <p v-if="costDetailModal.loading" class="cost-detail-loading">
          {{ t('components.logs.loading') }}
        </p>
        <div v-else-if="costDetailModal.data.length === 0" class="cost-detail-empty">
          {{ t('components.logs.costDetail.empty') }}
        </div>
        <ul v-else class="cost-detail-list">
          <li v-for="item in costDetailModal.data" :key="item.provider" class="cost-detail-item">
            <span class="cost-detail-item__name">{{ item.provider }}</span>
            <span class="cost-detail-item__value">{{ formatCurrency(item.cost_total) }}</span>
          </li>
        </ul>
      </div>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, onMounted, watch, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import {
  fetchRequestLogs,
  fetchLogProviders,
  fetchLogStats,
  fetchProviderDailyStats,
  type RequestLog,
  type LogStats,
  type LogStatsSeries,
  type LogPlatform,
  type ProviderDailyStat,
} from '../../services/logs'
import {
  Chart,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
  Legend,
} from 'chart.js'
import type { ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'

Chart.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend)

const { t } = useI18n()
const router = useRouter()

const logs = ref<RequestLog[]>([])
const stats = ref<LogStats | null>(null)
const loading = ref(false)
const filters = reactive<{ platform: LogPlatform | ''; provider: string }>({ platform: '', provider: '' })
const page = ref(1)
const PAGE_SIZE = 15
const providerOptions = ref<string[]>([])
const statsSeries = computed<LogStatsSeries[]>(() => stats.value?.series ?? [])

// 金额明细弹窗状态
const costDetailModal = reactive<{
  open: boolean
  loading: boolean
  data: ProviderDailyStat[]
}>({
  open: false,
  loading: false,
  data: [],
})

// 打开金额明细弹窗
const openCostDetailModal = async () => {
  costDetailModal.open = true
  costDetailModal.loading = true
  costDetailModal.data = []

  try {
    const stats = await fetchProviderDailyStats(filters.platform)
    // 按金额降序排序，过滤掉金额为 0 的
    costDetailModal.data = (stats ?? [])
      .filter(item => item.cost_total > 0)
      .sort((a, b) => b.cost_total - a.cost_total)
  } catch (error) {
    console.error('failed to load provider daily stats', error)
  } finally {
    costDetailModal.loading = false
  }
}

// 关闭金额明细弹窗
const closeCostDetailModal = () => {
  costDetailModal.open = false
}

const parseLogDate = (value?: string) => {
  if (!value) return null
  const normalize = value.replace(' ', 'T')
  const attempts = [value, `${normalize}`, `${normalize}Z`]
  for (const candidate of attempts) {
    const parsed = new Date(candidate)
    if (!Number.isNaN(parsed.getTime())) {
      return parsed
    }
  }
  const match = value.match(/^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2}) ([+-]\d{4}) UTC$/)
  if (match) {
    const [, day, time, zone] = match
    const zoneFormatted = `${zone.slice(0, 3)}:${zone.slice(3)}`
    const parsed = new Date(`${day}T${time}${zoneFormatted}`)
    if (!Number.isNaN(parsed.getTime())) {
      return parsed
    }
  }
  return null
}

const chartData = computed(() => {
  const series = statsSeries.value
  return {
    labels: series.map((item) => formatSeriesLabel(item.day)),
    datasets: [
      {
        label: t('components.logs.tokenLabels.cost'),
        data: series.map((item) => Number(((item.total_cost ?? 0)).toFixed(4))),
        borderColor: '#f97316',
        backgroundColor: 'rgba(249, 115, 22, 0.2)',
        tension: 0.3,
        fill: false,
        yAxisID: 'yCost',
      },
      {
        label: t('components.logs.tokenLabels.input'),
        data: series.map((item) => item.input_tokens ?? 0),
        borderColor: '#34d399',
        backgroundColor: 'rgba(52, 211, 153, 0.25)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.output'),
        data: series.map((item) => item.output_tokens ?? 0),
        borderColor: '#60a5fa',
        backgroundColor: 'rgba(96, 165, 250, 0.2)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.reasoning'),
        data: series.map((item) => item.reasoning_tokens ?? 0),
        borderColor: '#f472b6',
        backgroundColor: 'rgba(244, 114, 182, 0.2)',
        tension: 0.35,
        fill: true,
      },
      {
        label: t('components.logs.tokenLabels.cacheWrite'),
        data: series.map((item) => item.cache_create_tokens ?? 0),
        borderColor: '#fbbf24',
        backgroundColor: 'rgba(251, 191, 36, 0.2)',
        tension: 0.35,
        fill: false,
      },
      {
        label: t('components.logs.tokenLabels.cacheRead'),
        data: series.map((item) => item.cache_read_tokens ?? 0),
        borderColor: '#38bdf8',
        backgroundColor: 'rgba(56, 189, 248, 0.15)',
        tension: 0.35,
        fill: false,
      },
    ],
  }
})

const chartOptions: ChartOptions<'line'> = {
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    mode: 'index',
    intersect: false,
  },
  plugins: {
    legend: {
      labels: {
        color: '#0f172a',
        font: {
          size: 12,
          weight: 500,
        },
      },
    },
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: { color: '#94a3b8' },
    },
    y: {
      beginAtZero: true,
      ticks: { color: '#94a3b8' },
      grid: { color: 'rgba(148, 163, 184, 0.2)' },
    },
    yCost: {
      position: 'right',
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      ticks: {
        color: '#475569',
        callback: (value: string | number) => {
          const numeric = typeof value === 'number' ? value : Number(value)
          if (Number.isNaN(numeric)) return '$0'
          if (numeric >= 1) return `$${numeric.toFixed(2)}`
          return `$${numeric.toFixed(4)}`
        },
      },
    },
  },
}
const formatSeriesLabel = (value?: string) => {
  if (!value) return ''
  const parsed = parseLogDate(value)
  if (parsed) {
    return `${padHour(parsed.getHours())}:00`
  }
  const match = value.match(/(\d{2}):(\d{2})/)
  if (match) {
    return `${match[1]}:${match[2]}`
  }
  return value
}

const REFRESH_INTERVAL = 30
const countdown = ref(REFRESH_INTERVAL)
let timer: number | undefined

const resetTimer = () => {
  countdown.value = REFRESH_INTERVAL
}

const startCountdown = () => {
  stopCountdown()
  timer = window.setInterval(() => {
    if (countdown.value <= 1) {
      countdown.value = REFRESH_INTERVAL
      void loadDashboard()
    } else {
      countdown.value -= 1
    }
  }, 1000)
}

const stopCountdown = () => {
  if (timer) {
    clearInterval(timer)
    timer = undefined
  }
}

const loadLogs = async () => {
  loading.value = true
  try {
    const data = await fetchRequestLogs({
      platform: filters.platform,
      provider: filters.provider,
      limit: 200,
    })
    logs.value = data ?? []
    page.value = Math.min(page.value, totalPages.value)
  } catch (error) {
    console.error('failed to load request logs', error)
  } finally {
    loading.value = false
  }
}

const loadStats = async () => {
  try {
    const data = await fetchLogStats(filters.platform)
    stats.value = data ?? null
  } catch (error) {
    console.error('failed to load log stats', error)
  }
}

const loadDashboard = async () => {
  await Promise.all([loadLogs(), loadStats()])
}

const pagedLogs = computed(() => {
  const start = (page.value - 1) * PAGE_SIZE
  return logs.value.slice(start, start + PAGE_SIZE)
})

const totalPages = computed(() => Math.max(1, Math.ceil(logs.value.length / PAGE_SIZE)))

const applyFilters = async () => {
  page.value = 1
  await loadDashboard()
  resetTimer()
}

const refreshLogs = () => {
  void loadDashboard()
}

const manualRefresh = () => {
  resetTimer()
  void loadDashboard()
}

const nextPage = () => {
  if (page.value < totalPages.value) {
    page.value += 1
  }
}

const prevPage = () => {
  if (page.value > 1) {
    page.value -= 1
  }
}

const backToHome = () => {
  router.push('/')
}

const padHour = (num: number) => num.toString().padStart(2, '0')

const formatTime = (value?: string) => {
  const date = parseLogDate(value)
  if (!date) return value || '—'
  return `${date.getFullYear()}-${padHour(date.getMonth() + 1)}-${padHour(date.getDate())} ${padHour(date.getHours())}:${padHour(date.getMinutes())}:${padHour(date.getSeconds())}`
}

const formatStream = (value?: boolean | number) => {
  const isOn = value === true || value === 1
  return isOn ? t('components.logs.streamOn') : t('components.logs.streamOff')
}

const formatDuration = (value?: number) => {
  if (!value || Number.isNaN(value)) return '—'
  return `${value.toFixed(2)}s`
}

const httpCodeClass = (code: number) => {
  if (code >= 500) return 'http-server-error'
  if (code >= 400) return 'http-client-error'
  if (code >= 300) return 'http-redirect'
  if (code >= 200) return 'http-success'
  return 'http-info'
}

const durationColor = (value?: number) => {
  if (!value || Number.isNaN(value)) return 'neutral'
  if (value < 2) return 'fast'
  if (value < 5) return 'medium'
  return 'slow'
}

const formatNumber = (value?: number) => {
  if (value === undefined || value === null) return '—'
  return value.toLocaleString()
}

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

const startOfTodayLocal = () => {
  const now = new Date()
  now.setHours(0, 0, 0, 0)
  return now
}

const statsCards = computed(() => {
  const data = stats.value
  const summaryDate = summaryDateLabel.value
  const totalTokens =
    (data?.input_tokens ?? 0) + (data?.output_tokens ?? 0) + (data?.reasoning_tokens ?? 0)
  return [
    {
      key: 'requests',
      label: t('components.logs.summary.total'),
      hint: t('components.logs.summary.requests'),
      value: data ? formatNumber(data.total_requests) : '—',
    },
    {
      key: 'tokens',
      label: t('components.logs.summary.tokens'),
      hint: t('components.logs.summary.tokenHint'),
      value: data ? formatNumber(totalTokens) : '—',
    },
    {
      key: 'cacheReads',
      label: t('components.logs.summary.cache'),
      hint: t('components.logs.summary.cacheHint'),
      value: data ? formatNumber(data.cache_read_tokens) : '—',
    },
    {
      key: 'cost',
      label: t('components.logs.tokenLabels.cost'),
      hint: summaryDate ? t('components.logs.summary.todayScope', { date: summaryDate }) : '',
      value: formatCurrency(data?.cost_total ?? 0),
    },
  ]
})

const summaryDateLabel = computed(() => {
  const firstBucket = statsSeries.value.find((item) => item.day)
  const parsed = parseLogDate(firstBucket?.day ?? '')
  const date = parsed ?? startOfTodayLocal()
  return `${date.getFullYear()}-${padHour(date.getMonth() + 1)}-${padHour(date.getDate())}`
})

const loadProviderOptions = async () => {
  try {
    const list = await fetchLogProviders(filters.platform)
    providerOptions.value = list ?? []
    if (filters.provider && !providerOptions.value.includes(filters.provider)) {
      filters.provider = ''
    }
  } catch (error) {
    console.error('failed to load provider options', error)
  }
}

watch(
  () => filters.platform,
  async () => {
    await loadProviderOptions()
  },
)

onMounted(async () => {
  await Promise.all([loadDashboard(), loadProviderOptions()])
  startCountdown()
})

onUnmounted(() => {
  stopCountdown()
})
</script>

<style scoped>
.logs-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 1rem;
  margin-bottom: 0.75rem;
}

.summary-meta {
  grid-column: 1 / -1;
  font-size: 0.85rem;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: #64748b;
}

.summary-card {
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 16px;
  padding: 1rem 1.25rem;
  background: radial-gradient(circle at top, rgba(148, 163, 184, 0.1), rgba(15, 23, 42, 0));
  backdrop-filter: blur(6px);
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}

.summary-card__label {
  font-size: 0.85rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: #475569;
}

.summary-card__value {
  font-size: 1.85rem;
  font-weight: 600;
  color: #0f172a;
}

.summary-card__hint {
  font-size: 0.85rem;
  color: #94a3b8;
}

html.dark .summary-card {
  border-color: rgba(255, 255, 255, 0.12);
  background: radial-gradient(circle at top, rgba(148, 163, 184, 0.2), rgba(15, 23, 42, 0.35));
}

html.dark .summary-card__label {
  color: rgba(248, 250, 252, 0.75);
}

html.dark .summary-card__value {
  color: rgba(248, 250, 252, 0.95);
}

html.dark .summary-card__hint {
  color: rgba(186, 194, 210, 0.8);
}

@media (max-width: 768px) {
  .logs-summary {
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  }
}

/* 可点击卡片 */
.summary-card--clickable {
  cursor: pointer;
  transition: transform 0.15s ease, box-shadow 0.15s ease;
}
.summary-card--clickable:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(249, 115, 22, 0.15);
}
.summary-card--clickable:active {
  transform: translateY(0);
}
html.dark .summary-card--clickable:hover {
  box-shadow: 0 4px 12px rgba(249, 115, 22, 0.25);
}

/* 弹窗内容 */
.cost-detail-modal {
  min-height: 120px;
}
.cost-detail-loading,
.cost-detail-empty {
  text-align: center;
  color: #64748b;
  padding: 2rem 0;
}
html.dark .cost-detail-loading,
html.dark .cost-detail-empty {
  color: #94a3b8;
}
.cost-detail-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.cost-detail-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1rem;
  background: rgba(148, 163, 184, 0.08);
  border-radius: 8px;
  transition: background 0.15s ease;
}
.cost-detail-item:hover {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .cost-detail-item {
  background: rgba(148, 163, 184, 0.12);
}
html.dark .cost-detail-item:hover {
  background: rgba(148, 163, 184, 0.18);
}
.cost-detail-item__name {
  font-weight: 500;
  color: #1e293b;
}
html.dark .cost-detail-item__name {
  color: #f1f5f9;
}
.cost-detail-item__value {
  font-weight: 600;
  color: #f97316;
  font-variant-numeric: tabular-nums;
}
</style>
