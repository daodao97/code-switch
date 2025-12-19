<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CheckEnvConflicts
} from '../../../bindings/codeswitch/services/envcheckservice'
import type { EnvConflict } from '../../../bindings/codeswitch/services/models'
import { extractErrorMessage } from '../../utils/error'

const { t } = useI18n()

type Platform = 'claude' | 'codex' | 'gemini'

const platforms: { id: Platform; name: string }[] = [
  { id: 'claude', name: 'Claude Code' },
  { id: 'codex', name: 'Codex' },
  { id: 'gemini', name: 'Gemini' }
]

const activePlatform = ref<Platform>('claude')
const conflicts = ref<EnvConflict[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

const conflictCount = computed(() => conflicts.value.length)
const hasConflicts = computed(() => conflictCount.value > 0)

async function checkConflicts() {
  loading.value = true
  error.value = null
  try {
    conflicts.value = await CheckEnvConflicts(activePlatform.value)
  } catch (e) {
    console.error('Failed to check conflicts:', e)
    error.value = extractErrorMessage(e)
    conflicts.value = []
  } finally {
    loading.value = false
  }
}

function getSourceIcon(sourceType: 'system' | 'file'): string {
  return sourceType === 'system' ? 'desktop' : 'file'
}

function getSourceLabel(conflict: EnvConflict): string {
  if (conflict.sourceType === 'system') {
    return t('envcheck.source.system')
  }
  return conflict.sourcePath
}

function maskValue(value: string): string {
  if (value.length <= 8) return '••••••••'
  return value.substring(0, 4) + '••••' + value.substring(value.length - 4)
}

watch(activePlatform, () => {
  checkConflicts()
})

onMounted(() => {
  checkConflicts()
})
</script>

<template>
  <div class="envcheck-page">
    <!-- Hero Section -->
    <div class="page-hero">
      <p class="hero-eyebrow">{{ t('envcheck.hero.eyebrow') }}</p>
      <h1 class="hero-title">{{ t('envcheck.hero.title') }}</h1>
      <p class="hero-lead">{{ t('envcheck.hero.lead') }}</p>
    </div>

    <!-- Platform Tabs -->
    <div class="platform-tabs">
      <button
        v-for="platform in platforms"
        :key="platform.id"
        class="platform-tab"
        :class="{ active: activePlatform === platform.id }"
        @click="activePlatform = platform.id"
      >
        {{ platform.name }}
      </button>
    </div>

    <!-- Status Banner -->
    <div
      class="status-banner"
      :class="{
        warning: hasConflicts,
        success: !hasConflicts && !loading && !error
      }"
    >
      <svg v-if="hasConflicts" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path>
        <line x1="12" y1="9" x2="12" y2="13"></line>
        <line x1="12" y1="17" x2="12.01" y2="17"></line>
      </svg>
      <svg v-else-if="!loading && !error" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
        <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path>
        <polyline points="22 4 12 14.01 9 11.01"></polyline>
      </svg>
      <span v-if="loading">{{ t('envcheck.checking') }}</span>
      <span v-else-if="error">{{ t('envcheck.error') }}</span>
      <span v-else-if="hasConflicts">
        {{ t('envcheck.found', { count: conflictCount }) }}
      </span>
      <span v-else>{{ t('envcheck.noConflicts') }}</span>
    </div>

    <!-- Conflict List -->
    <div class="conflict-list" v-if="!loading">
      <div v-if="error" class="error-state">
        <p>{{ error }}</p>
      </div>

      <div
        v-for="(conflict, index) in conflicts"
        :key="index"
        class="conflict-card"
      >
        <div class="conflict-header">
          <span class="conflict-var">{{ conflict.varName }}</span>
          <span class="conflict-source-badge" :class="conflict.sourceType">
            <svg v-if="conflict.sourceType === 'system'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <rect x="2" y="3" width="20" height="14" rx="2" ry="2"></rect>
              <line x1="8" y1="21" x2="16" y2="21"></line>
              <line x1="12" y1="17" x2="12" y2="21"></line>
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
              <polyline points="14 2 14 8 20 8"></polyline>
            </svg>
          </span>
        </div>

        <div class="conflict-details">
          <div class="detail-row">
            <span class="detail-label">{{ t('envcheck.value') }}:</span>
            <code class="detail-value">{{ maskValue(conflict.varValue) }}</code>
          </div>
          <div class="detail-row">
            <span class="detail-label">{{ t('envcheck.source') }}:</span>
            <span class="detail-value source-path">{{ getSourceLabel(conflict) }}</span>
          </div>
        </div>
      </div>
    </div>

    <div v-else class="loading-state">
      <div class="spinner"></div>
      <span>{{ t('envcheck.checking') }}</span>
    </div>

    <!-- Refresh Button -->
    <div class="page-actions">
      <button class="refresh-btn" @click="checkConflicts" :disabled="loading">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" :class="{ spin: loading }">
          <polyline points="23 4 23 10 17 10"></polyline>
          <polyline points="1 20 1 14 7 14"></polyline>
          <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"></path>
        </svg>
        {{ t('envcheck.refresh') }}
      </button>
    </div>
  </div>
</template>

<style scoped>
.envcheck-page {
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

.platform-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
  padding: 4px;
  background: var(--mac-surface);
  border-radius: 12px;
  border: 1px solid var(--mac-border);
}

.platform-tab {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 10px 16px;
  border: none;
  background: transparent;
  border-radius: 8px;
  font-size: 0.9rem;
  font-weight: 500;
  color: var(--mac-text-secondary);
  cursor: pointer;
  transition: all 0.15s ease;
}

.platform-tab:hover {
  color: var(--mac-text);
  background: rgba(15, 23, 42, 0.05);
}

html.dark .platform-tab:hover {
  background: rgba(255, 255, 255, 0.08);
}

.platform-tab.active {
  background: var(--mac-accent);
  color: #fff;
}

.status-banner {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 20px;
  border-radius: 12px;
  margin-bottom: 24px;
  font-size: 0.95rem;
  font-weight: 500;
}

.status-banner svg {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
}

.status-banner.warning {
  background: rgba(245, 158, 11, 0.1);
  color: #f59e0b;
  border: 1px solid rgba(245, 158, 11, 0.2);
}

.status-banner.success {
  background: rgba(16, 185, 129, 0.1);
  color: #10b981;
  border: 1px solid rgba(16, 185, 129, 0.2);
}

.conflict-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 24px;
}

.conflict-card {
  padding: 20px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 16px;
  transition: all 0.15s ease;
}

.conflict-card:hover {
  border-color: var(--mac-accent);
}

.conflict-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.conflict-var {
  font-size: 1rem;
  font-weight: 600;
  color: var(--mac-text);
  font-family: 'SFMono-Regular', Menlo, Consolas, monospace;
}

.conflict-source-badge {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 8px;
}

.conflict-source-badge svg {
  width: 16px;
  height: 16px;
}

.conflict-source-badge.system {
  background: rgba(10, 132, 255, 0.1);
  color: var(--mac-accent);
}

.conflict-source-badge.file {
  background: rgba(245, 158, 11, 0.1);
  color: #f59e0b;
}

.conflict-details {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.detail-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.detail-label {
  font-size: 0.8rem;
  color: var(--mac-text-secondary);
  min-width: 60px;
}

.detail-value {
  font-size: 0.85rem;
  color: var(--mac-text);
}

.detail-value code,
code.detail-value {
  padding: 4px 8px;
  background: rgba(15, 23, 42, 0.05);
  border-radius: 6px;
  font-family: 'SFMono-Regular', Menlo, Consolas, monospace;
}

html.dark code.detail-value {
  background: rgba(255, 255, 255, 0.08);
}

.source-path {
  font-family: 'SFMono-Regular', Menlo, Consolas, monospace;
  word-break: break-all;
}

.loading-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  padding: 48px 24px;
  color: var(--mac-text-secondary);
}

.spinner {
  width: 32px;
  height: 32px;
  border: 3px solid var(--mac-border);
  border-top-color: var(--mac-accent);
  border-radius: 50%;
  animation: spin 1s linear infinite;
}

@keyframes spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}

.error-state {
  text-align: center;
  padding: 24px;
  color: #ef4444;
}

.page-actions {
  display: flex;
  justify-content: center;
}

.page-actions .refresh-btn {
  display: inline-flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  padding: 12px 24px;
  border: 1px solid var(--mac-border);
  border-radius: 999px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  white-space: nowrap;
}

.refresh-btn:hover:not(:disabled) {
  border-color: var(--mac-accent);
}

.refresh-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.refresh-btn svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

.refresh-btn svg.spin {
  animation: spin 1s linear infinite;
}
</style>
