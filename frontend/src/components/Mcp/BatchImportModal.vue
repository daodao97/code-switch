<template>
  <FullScreenPanel
    :open="open"
    :title="t('components.mcp.import.title')"
    @close="handleClose"
  >
    <div class="import-container">
      <!-- Step 1: JSON Input -->
      <div v-if="step === 'input'">
        <p class="step-desc">{{ t('components.mcp.import.inputDesc') }}</p>
        <BaseTextarea
          ref="textareaRef"
          v-model="jsonInput"
          :placeholder="t('components.mcp.import.placeholder')"
          rows="12"
          class="json-input"
        />
        <p v-if="error" class="alert-error">{{ error }}</p>
        <div class="step-actions">
          <BaseButton variant="outline" @click="handleClose">
            {{ t('components.mcp.form.actions.cancel') }}
          </BaseButton>
          <BaseButton :disabled="!jsonInput.trim() || parsing" @click="parseJson">
            {{ parsing ? t('components.mcp.import.parsing') : t('components.mcp.import.next') }}
          </BaseButton>
        </div>
      </div>

      <!-- Step 2: Review & Select -->
      <div v-else-if="step === 'review'">
        <div class="review-header">
          <h3>{{ t('components.mcp.import.reviewTitle', { count: parsedServers.length }) }}</h3>
          <label class="select-all-label">
            <input type="checkbox" :checked="allSelected" @change="toggleAll" />
            <span>{{ t('components.mcp.import.selectAll') }}</span>
          </label>
        </div>

        <div v-if="hasConflicts" class="conflict-notice">
          <span class="conflict-icon">!</span>
          <span>{{ t('components.mcp.import.conflictNotice', { count: conflictCount }) }}</span>
          <select v-model="conflictStrategy" class="strategy-select">
            <option value="skip">{{ t('components.mcp.import.strategySkip') }}</option>
            <option value="overwrite">{{ t('components.mcp.import.strategyOverwrite') }}</option>
          </select>
        </div>

        <div class="server-list">
          <div
            v-for="server in parsedServers"
            :key="server.data.name"
            class="server-item"
            :class="{ 'is-selected': server.selected, 'is-conflict': server.isConflict }"
            @click="toggleSelection(server)"
          >
            <div class="checkbox-wrapper">
              <input type="checkbox" :checked="server.selected" @click.stop />
            </div>
            <div class="server-info">
              <div class="server-name-row">
                <span class="server-name">{{ server.data.name }}</span>
                <span v-if="server.isConflict" class="badge conflict">
                  {{ t('components.mcp.import.status.conflict') }}
                </span>
                <span v-else class="badge new">
                  {{ t('components.mcp.import.status.new') }}
                </span>
              </div>
              <div class="server-detail">
                <span class="type-tag">{{ server.data.type }}</span>
                <span class="detail-text">
                  {{ server.data.type === 'http' ? server.data.url : server.data.command }}
                </span>
              </div>
            </div>
          </div>
        </div>

        <div class="step-actions">
          <BaseButton variant="outline" @click="goBack">
            {{ t('components.mcp.import.back') }}
          </BaseButton>
          <BaseButton :disabled="selectedCount === 0 || importing" @click="doImport">
            {{ importing ? t('components.mcp.import.importing') : t('components.mcp.import.submit', { count: selectedCount }) }}
          </BaseButton>
        </div>
      </div>
    </div>
  </FullScreenPanel>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import FullScreenPanel from '../common/FullScreenPanel.vue'
import BaseButton from '../common/BaseButton.vue'
import BaseTextarea from '../common/BaseTextarea.vue'
import {
  parseMcpJSON,
  importMcpServers,
  type McpServer,
  type ConflictStrategy,
} from '../../services/mcp'
import { showToast } from '../../utils/toast'

interface ParsedServerItem {
  data: McpServer
  selected: boolean
  isConflict: boolean
}

const props = defineProps<{
  open: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'imported', count: number): void
}>()

const { t } = useI18n()

const step = ref<'input' | 'review'>('input')
const jsonInput = ref('')
const error = ref('')
const parsing = ref(false)
const importing = ref(false)
const parsedServers = ref<ParsedServerItem[]>([])
const conflictNames = ref<string[]>([])
const conflictStrategy = ref<ConflictStrategy>('skip')
const textareaRef = ref<InstanceType<typeof BaseTextarea> | null>(null)

const selectedCount = computed(() => parsedServers.value.filter((s) => s.selected).length)
const allSelected = computed(
  () => parsedServers.value.length > 0 && parsedServers.value.every((s) => s.selected)
)
const hasConflicts = computed(() => conflictNames.value.length > 0)
const conflictCount = computed(() => conflictNames.value.length)

watch(
  () => props.open,
  (isOpen) => {
    if (isOpen) {
      resetState()
      nextTick(() => textareaRef.value?.focus())
    }
  }
)

const resetState = () => {
  step.value = 'input'
  jsonInput.value = ''
  error.value = ''
  parsing.value = false
  importing.value = false
  parsedServers.value = []
  conflictNames.value = []
  conflictStrategy.value = 'skip'
}

const handleClose = () => {
  emit('close')
}

const toggleAll = (e: Event) => {
  const checked = (e.target as HTMLInputElement).checked
  parsedServers.value.forEach((s) => (s.selected = checked))
}

const toggleSelection = (server: ParsedServerItem) => {
  server.selected = !server.selected
}

const goBack = () => {
  step.value = 'input'
  error.value = ''
}

const parseJson = async () => {
  error.value = ''
  parsing.value = true

  try {
    const result = await parseMcpJSON(jsonInput.value)
    if (!result || result.servers.length === 0) {
      error.value = t('components.mcp.import.noServers')
      return
    }

    if (result.needName) {
      error.value = t('components.mcp.import.needName')
      return
    }

    conflictNames.value = result.conflicts ?? []
    const conflictSet = new Set(conflictNames.value.map((n) => n.toLowerCase()))

    parsedServers.value = result.servers.map((server) => {
      const isConflict = conflictSet.has(server.name.toLowerCase())
      return {
        data: server,
        selected: !isConflict,
        isConflict,
      }
    })

    step.value = 'review'
  } catch (e) {
    error.value = e instanceof Error ? e.message : t('components.mcp.import.parseError')
  } finally {
    parsing.value = false
  }
}

const doImport = async () => {
  const selected = parsedServers.value.filter((s) => s.selected).map((s) => s.data)
  if (selected.length === 0) return

  importing.value = true
  try {
    const count = await importMcpServers(selected, conflictStrategy.value)
    if (count === 0) {
      showToast(t('components.mcp.import.allSkipped'), 'warning')
    } else {
      showToast(t('components.mcp.import.success', { count }), 'success')
    }
    emit('imported', count)
    emit('close')
  } catch (e) {
    showToast(e instanceof Error ? e.message : t('components.mcp.import.importError'), 'error')
  } finally {
    importing.value = false
  }
}
</script>

<style scoped>
.import-container {
  max-width: 800px;
  margin: 0 auto;
  padding-bottom: 40px;
}

.step-desc {
  margin-bottom: 1rem;
  color: var(--text-secondary, rgba(255, 255, 255, 0.7));
  line-height: 1.6;
}

.json-input {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 13px;
}

.alert-error {
  margin-top: 1rem;
  padding: 0.75rem 1rem;
  border-radius: 8px;
  background: rgba(244, 67, 54, 0.15);
  color: #ff9b9b;
  border: 1px solid rgba(244, 67, 54, 0.2);
}

.step-actions {
  display: flex;
  justify-content: flex-end;
  gap: 1rem;
  margin-top: 2rem;
  padding-top: 1rem;
  border-top: 1px solid var(--border-color, rgba(255, 255, 255, 0.1));
}

.review-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 1rem;
}

.review-header h3 {
  margin: 0;
  font-size: 1.25rem;
  font-weight: 600;
}

.select-all-label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.9rem;
  cursor: pointer;
  user-select: none;
}

.conflict-notice {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 1rem;
  padding: 0.75rem 1rem;
  border-radius: 8px;
  background: rgba(251, 191, 36, 0.1);
  border: 1px solid rgba(251, 191, 36, 0.25);
  color: #fbbf24;
  font-size: 0.9rem;
}

.conflict-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  background: rgba(251, 191, 36, 0.2);
  font-weight: 700;
  font-size: 12px;
}

.strategy-select {
  margin-left: auto;
  padding: 4px 8px;
  border-radius: 6px;
  border: 1px solid rgba(255, 255, 255, 0.15);
  background: rgba(255, 255, 255, 0.08);
  color: inherit;
  font-size: 0.85rem;
  cursor: pointer;
}

.server-list {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  max-height: 50vh;
  overflow-y: auto;
}

.server-item {
  display: flex;
  align-items: flex-start;
  gap: 1rem;
  padding: 1rem;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.server-item:hover {
  background: rgba(255, 255, 255, 0.06);
}

.server-item.is-selected {
  background: rgba(74, 222, 128, 0.05);
  border-color: rgba(74, 222, 128, 0.2);
}

.server-item.is-conflict {
  border-color: rgba(251, 191, 36, 0.3);
}

.checkbox-wrapper {
  padding-top: 0.25rem;
}

.server-info {
  flex: 1;
  min-width: 0;
}

.server-name-row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-bottom: 0.25rem;
}

.server-name {
  font-weight: 600;
  font-size: 1rem;
}

.badge {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  text-transform: uppercase;
  font-weight: 600;
}

.badge.new {
  background: rgba(74, 222, 128, 0.2);
  color: #4ade80;
}

.badge.conflict {
  background: rgba(251, 191, 36, 0.2);
  color: #fbbf24;
}

.server-detail {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 0.85rem;
  color: var(--text-secondary, rgba(255, 255, 255, 0.6));
  margin-top: 0.25rem;
}

.type-tag {
  background: rgba(255, 255, 255, 0.1);
  padding: 1px 6px;
  border-radius: 4px;
  font-size: 10px;
  flex-shrink: 0;
}

.detail-text {
  font-family: ui-monospace, SFMono-Regular, monospace;
  word-break: break-all;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
