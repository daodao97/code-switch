<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  TestEndpoints
} from '../../../bindings/codeswitch/services/speedtestservice'
import type { EndpointLatency } from '../../../bindings/codeswitch/services/models'

const { t } = useI18n()

interface Endpoint {
  url: string
  result: EndpointLatency | null
  testing: boolean
}

const newUrl = ref('')
const endpoints = ref<Endpoint[]>([
  { url: 'https://api.anthropic.com', result: null, testing: false },
  { url: 'https://api.openai.com', result: null, testing: false }
])
const isTesting = ref(false)

const endpointCount = computed(() => endpoints.value.length)

function addEndpoint() {
  if (!newUrl.value.trim()) return

  // 基础 URL 校验
  try {
    new URL(newUrl.value)
  } catch {
    return
  }

  // 检查重复
  if (endpoints.value.some(e => e.url === newUrl.value)) {
    return
  }

  endpoints.value.push({
    url: newUrl.value,
    result: null,
    testing: false
  })
  newUrl.value = ''
}

function removeEndpoint(index: number) {
  endpoints.value.splice(index, 1)
}

async function runTest() {
  if (isTesting.value || endpoints.value.length === 0) return

  isTesting.value = true

  // 标记所有为测试中
  endpoints.value.forEach(e => {
    e.testing = true
    e.result = null
  })

  try {
    const urls = endpoints.value.map(e => e.url)
    const results = await TestEndpoints(urls, 10)

    // 匹配结果
    results.forEach(result => {
      const endpoint = endpoints.value.find(e => e.url === result.url)
      if (endpoint) {
        endpoint.result = result
        endpoint.testing = false
      }
    })
  } catch (e) {
    console.error('Test failed:', e)
    endpoints.value.forEach(ep => {
      ep.testing = false
    })
  } finally {
    isTesting.value = false
  }
}

function getLatencyColor(latency: number | null | undefined): string {
  if (latency == null) return '#ef4444' // red for error
  if (latency < 300) return '#10b981' // green
  if (latency < 500) return '#f59e0b' // yellow
  if (latency < 800) return '#f97316' // orange
  return '#ef4444' // red
}

function getLatencyText(result: EndpointLatency | null): string {
  if (!result) return '-'
  if (result.latency == null) {
    return result.error || t('speedtest.failed')
  }
  return `${result.latency}ms`
}
</script>

<template>
  <div class="speedtest-page">
    <!-- Hero Section -->
    <div class="page-hero">
      <p class="hero-eyebrow">{{ t('speedtest.hero.eyebrow') }}</p>
      <h1 class="hero-title">{{ t('speedtest.hero.title') }}</h1>
      <p class="hero-lead">{{ t('speedtest.hero.lead') }}</p>
    </div>

    <!-- URL Input -->
    <div class="input-section">
      <input
        v-model="newUrl"
        type="url"
        class="url-input"
        :placeholder="t('speedtest.placeholder')"
        @keyup.enter="addEndpoint"
      />
      <button class="add-btn" @click="addEndpoint" :disabled="!newUrl.trim()">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="12" y1="5" x2="12" y2="19"></line>
          <line x1="5" y1="12" x2="19" y2="12"></line>
        </svg>
        {{ t('speedtest.add') }}
      </button>
    </div>

    <!-- Endpoint List Header -->
    <div class="list-header">
      <span class="list-title">
        {{ t('speedtest.endpoints', { count: endpointCount }) }}
      </span>
      <button
        class="test-btn"
        :class="{ testing: isTesting }"
        @click="runTest"
        :disabled="isTesting || endpointCount === 0"
      >
        <svg v-if="!isTesting" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
        </svg>
        <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="spin">
          <circle cx="12" cy="12" r="10"></circle>
          <path d="M12 6v6l4 2"></path>
        </svg>
        {{ isTesting ? t('speedtest.testing') : t('speedtest.start') }}
      </button>
    </div>

    <!-- Endpoint List -->
    <div class="endpoint-list">
      <div v-if="endpoints.length === 0" class="empty-state">
        <p>{{ t('speedtest.empty') }}</p>
      </div>

      <div
        v-for="(endpoint, index) in endpoints"
        :key="endpoint.url"
        class="endpoint-card"
      >
        <div class="endpoint-url">{{ endpoint.url }}</div>

        <div class="endpoint-result">
          <span
            v-if="endpoint.testing"
            class="result-testing"
          >
            {{ t('speedtest.testing') }}...
          </span>
          <span
            v-else-if="endpoint.result"
            class="result-latency"
            :style="{ color: getLatencyColor(endpoint.result.latency) }"
          >
            <span class="latency-dot" :style="{ background: getLatencyColor(endpoint.result.latency) }"></span>
            {{ getLatencyText(endpoint.result) }}
          </span>
          <span v-else class="result-pending">-</span>
        </div>

        <button class="remove-btn" @click="removeEndpoint(index)">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18"></line>
            <line x1="6" y1="6" x2="18" y2="18"></line>
          </svg>
        </button>
      </div>
    </div>

    <!-- Legend -->
    <div class="legend">
      <div class="legend-item">
        <span class="legend-dot" style="background: #10b981;"></span>
        <span>&lt; 300ms</span>
      </div>
      <div class="legend-item">
        <span class="legend-dot" style="background: #f59e0b;"></span>
        <span>300-500ms</span>
      </div>
      <div class="legend-item">
        <span class="legend-dot" style="background: #f97316;"></span>
        <span>500-800ms</span>
      </div>
      <div class="legend-item">
        <span class="legend-dot" style="background: #ef4444;"></span>
        <span>&gt; 800ms / {{ t('speedtest.failed') }}</span>
      </div>
    </div>
  </div>
</template>

<style scoped>
.speedtest-page {
  padding: 24px;
  max-width: 800px;
  margin: 0 auto;
}

.page-hero {
  margin-bottom: 32px;
}

.hero-eyebrow {
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  color: var(--mac-accent);
  margin-bottom: 8px;
}

.hero-title {
  font-size: 1.75rem;
  font-weight: 700;
  color: var(--mac-text);
  margin-bottom: 8px;
}

.hero-lead {
  font-size: 0.95rem;
  color: var(--mac-text-secondary);
  line-height: 1.5;
}

.input-section {
  display: flex;
  gap: 12px;
  margin-bottom: 24px;
}

.url-input {
  flex: 1;
  padding: 12px 16px;
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  font-size: 0.9rem;
  background: var(--mac-surface);
  color: var(--mac-text);
  transition: all 0.15s ease;
}

.url-input:focus {
  outline: none;
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 3px rgba(10, 132, 255, 0.15);
}

.add-btn {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  white-space: nowrap;
  flex-wrap: nowrap;
}

.add-btn:hover:not(:disabled) {
  border-color: var(--mac-accent);
}

.add-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.add-btn svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.add-btn {
  justify-content: center;
  line-height: 1;
}

.list-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.list-title {
  font-size: 0.9rem;
  color: var(--mac-text-secondary);
}

.test-btn {
  display: flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  padding: 10px 20px;
  border: none;
  border-radius: 999px;
  background: var(--mac-accent);
  color: #fff;
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  white-space: nowrap;
  flex-wrap: nowrap;
}

.test-btn:hover:not(:disabled) {
  opacity: 0.9;
}

.test-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.test-btn svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.test-btn {
  justify-content: center;
  line-height: 1;
}

.test-btn.testing svg.spin {
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.endpoint-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 24px;
}

.endpoint-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 16px;
  transition: all 0.15s ease;
}

.endpoint-card:hover {
  border-color: var(--mac-accent);
}

.endpoint-url {
  flex: 1;
  font-size: 0.9rem;
  color: var(--mac-text);
  font-family: 'SFMono-Regular', Menlo, Consolas, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.endpoint-result {
  min-width: 100px;
  text-align: right;
}

.result-testing {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.result-latency {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
  font-size: 0.9rem;
  font-weight: 600;
}

.latency-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
}

.result-pending {
  color: var(--mac-text-secondary);
}

.remove-btn {
  width: 32px;
  height: 32px;
  border: none;
  background: transparent;
  border-radius: 8px;
  color: var(--mac-text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
}

.remove-btn:hover {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.1);
}

.remove-btn svg {
  width: 16px;
  height: 16px;
}

.empty-state {
  text-align: center;
  padding: 48px 24px;
  color: var(--mac-text-secondary);
}

.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 24px;
  padding: 16px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 0.8rem;
  color: var(--mac-text-secondary);
}

.legend-dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
}
</style>
