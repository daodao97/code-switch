<template>
  <div class="cli-config-editor">
    <div class="cli-header" @click="toggleExpanded">
      <div class="cli-header-left">
        <svg
          class="expand-icon"
          :class="{ expanded }"
          viewBox="0 0 20 20"
          aria-hidden="true"
        >
          <path
            d="M6 8l4 4 4-4"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
        </svg>
        <span class="cli-title">{{ t('components.cliConfig.title') }}</span>
        <span class="cli-platform-badge">{{ platformLabel }}</span>
      </div>
      <div class="cli-header-right" @click.stop>
        <button
          v-if="expanded"
          class="cli-action-btn"
          type="button"
          :title="t('components.cliConfig.restoreDefault')"
          @click="handleRestoreDefault"
        >
          <svg viewBox="0 0 20 20" aria-hidden="true">
            <path
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
              stroke-linejoin="round"
              fill="none"
            />
          </svg>
        </button>
      </div>
    </div>

    <div v-if="expanded" class="cli-content" @paste="handleSmartPaste">
      <div v-if="loading" class="cli-loading">
        {{ t('components.cliConfig.loading') }}
      </div>

      <template v-else-if="config">
        <!-- 锁定字段 -->
        <div class="cli-section">
          <div class="cli-section-header">
            <span class="lock-icon">🔒</span>
            <span>{{ t('components.cliConfig.lockedFields') }}</span>
          </div>
          <div class="cli-fields">
            <div
              v-for="field in lockedFields"
              :key="field.key"
              class="cli-field locked"
            >
              <label class="cli-field-label">{{ field.key }}</label>
              <input
                type="text"
                :value="field.value"
                disabled
                class="cli-field-input disabled"
              />
              <span v-if="field.hint" class="cli-field-hint">{{ field.hint }}</span>
            </div>
          </div>
        </div>

        <!-- 可编辑字段 -->
        <div class="cli-section">
          <div class="cli-section-header">
            <span class="edit-icon">✏️</span>
            <span>{{ t('components.cliConfig.editableFields') }}</span>
          </div>
          <div class="cli-fields">
            <div
              v-for="field in editableFields"
              :key="field.key"
              class="cli-field"
            >
              <label class="cli-field-label">{{ field.key }}</label>

              <!-- 布尔类型 -->
              <template v-if="field.type === 'boolean'">
                <label class="cli-switch">
                  <input
                    type="checkbox"
                    :checked="getFieldValue(field.key)"
                    @change="updateField(field.key, ($event.target as HTMLInputElement).checked)"
                  />
                  <span class="cli-switch-slider"></span>
                </label>
              </template>

              <!-- 对象类型（JSON 编辑器） -->
              <template v-else-if="field.type === 'object'">
                <textarea
                  :value="JSON.stringify(getFieldValue(field.key) || {}, null, 2)"
                  class="cli-field-textarea"
                  rows="3"
                  @change="updateFieldJSON(field.key, ($event.target as HTMLTextAreaElement).value)"
                />
              </template>

              <!-- 字符串类型 -->
              <template v-else>
                <input
                  type="text"
                  :value="getFieldValue(field.key) || ''"
                  class="cli-field-input"
                  @input="updateField(field.key, ($event.target as HTMLInputElement).value)"
                />
              </template>
            </div>
          </div>
        </div>

        <!-- 自定义字段 -->
        <div class="cli-section">
          <div class="cli-section-header">
            <span class="custom-icon">🔧</span>
            <span>{{ t('components.cliConfig.customFields') }}</span>
            <button
              type="button"
              class="cli-add-btn"
              @click="addCustomField"
              :title="t('components.cliConfig.addField')"
            >
              <svg viewBox="0 0 20 20" aria-hidden="true">
                <path
                  d="M10 5v10M5 10h10"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                />
              </svg>
            </button>
          </div>
          <div v-if="customFields.length === 0" class="cli-empty-hint">
            {{ t('components.cliConfig.noCustomFields') }}
          </div>
          <div v-else class="cli-fields">
            <div
              v-for="(field, index) in customFields"
              :key="field.id"
              class="cli-custom-field"
            >
              <input
                type="text"
                :value="field.keyDraft"
                class="cli-field-input cli-key-input"
                :placeholder="t('components.cliConfig.keyPlaceholder')"
                @input="updateCustomFieldKey(index, ($event.target as HTMLInputElement).value)"
                @blur="commitCustomFieldKey(index)"
              />
              <input
                type="text"
                :value="field.value"
                class="cli-field-input cli-value-input"
                :placeholder="t('components.cliConfig.valuePlaceholder')"
                @input="updateCustomFieldValue(index, ($event.target as HTMLInputElement).value)"
              />
              <button
                type="button"
                class="cli-delete-btn"
                @click="removeCustomField(index)"
                :title="t('components.cliConfig.removeField')"
              >
                <svg viewBox="0 0 20 20" aria-hidden="true">
                  <path
                    d="M6 6l8 8M6 14l8-8"
                    stroke="currentColor"
                    stroke-width="1.5"
                    stroke-linecap="round"
                  />
                </svg>
              </button>
            </div>
          </div>
        </div>

        <!-- 模板选项 -->
        <div class="cli-template-options">
          <label class="cli-checkbox">
            <input
              type="checkbox"
              v-model="isGlobalTemplate"
              @change="handleTemplateChange"
            />
            <span>{{ t('components.cliConfig.setAsTemplate') }}</span>
          </label>
        </div>

        <!-- 配置预览（可折叠） -->
        <div v-if="previewFiles.length || currentFiles.length" class="cli-preview-section">
          <div class="cli-preview-header" @click="togglePreview">
            <svg
              class="expand-icon"
              :class="{ expanded: previewExpanded }"
              viewBox="0 0 20 20"
              aria-hidden="true"
            >
              <path
                d="M6 8l4 4 4-4"
                stroke="currentColor"
                stroke-width="1.5"
                stroke-linecap="round"
                stroke-linejoin="round"
                fill="none"
              />
            </svg>
            <span class="preview-icon">👁️</span>
            <span>{{ t('components.cliConfig.previewTitle') }}</span>
            <span class="cli-preview-count">{{ previewFiles.length }}</span>
            <button
              v-if="previewExpanded && selectedPreviewTab === 0"
              type="button"
              class="cli-action-btn cli-preview-lock"
              @click.stop="togglePreviewEditable"
            >
              <span v-if="previewEditable">🔓 {{ t('components.cliConfig.previewEditUnlocked') }}</span>
              <span v-else>🔒 {{ t('components.cliConfig.previewEditLocked') }}</span>
            </button>
            <!-- Current 标签页解锁按钮 -->
            <button
              v-if="previewExpanded && selectedPreviewTab === 1"
              type="button"
              class="cli-action-btn cli-preview-lock"
              @click.stop="toggleCurrentEditable"
            >
              <span v-if="currentEditable">🔓 {{ t('components.cliConfig.previewEditUnlocked') }}</span>
              <span v-else>🔒 {{ t('components.cliConfig.previewEditLocked') }}</span>
            </button>
          </div>
          <div v-if="previewExpanded" class="cli-preview-tabs-wrapper">
            <TabGroup :selectedIndex="selectedPreviewTab" @change="selectedPreviewTab = $event">
              <TabList class="cli-tabs-list">
                <Tab as="template" v-slot="{ selected }">
                  <button :class="['cli-tab-btn', { selected }]">
                    {{ t('components.cliConfig.tabPreview') }}
                  </button>
                </Tab>
                <Tab as="template" v-slot="{ selected }">
                  <button :class="['cli-tab-btn', { selected }]">
                    {{ t('components.cliConfig.tabCurrent') }}
                  </button>
                </Tab>
              </TabList>
              <TabPanels>
                <!-- Preview Tab: 激活后的配置 -->
                <TabPanel class="cli-preview-list">
                  <div
                    v-for="(file, index) in previewFiles"
                    :key="getPreviewKey(file, index)"
                    class="cli-preview-card"
                  >
                    <div class="cli-preview-meta">
                      <span class="cli-preview-name">{{ file.path || t('components.cliConfig.previewUnknownPath') }}</span>
                      <span class="cli-preview-format">{{ (file.format || config?.configFormat || '').toUpperCase() }}</span>
                    </div>
                    <template v-if="previewEditable">
                      <textarea
                        :ref="index === 0 ? (el) => firstTextareaRef = el as HTMLTextAreaElement : undefined"
                        v-model="editingContent[getPreviewKey(file, index)]"
                        class="cli-preview-textarea"
                        rows="8"
                      />
                      <div class="cli-preview-actions">
                        <button
                          type="button"
                          class="cli-action-btn cli-primary-btn"
                          :disabled="previewSaving"
                          @click="handleApplyPreviewEdit(file, index)"
                        >
                          {{ t('components.cliConfig.previewApply') }}
                        </button>
                        <button
                          type="button"
                          class="cli-action-btn"
                          :disabled="previewSaving"
                          @click="handleResetPreviewEdit(file, index)"
                        >
                          {{ t('components.cliConfig.previewReset') }}
                        </button>
                      </div>
                      <div
                        v-if="previewErrors[getPreviewKey(file, index)]"
                        class="cli-preview-error"
                      >
                        {{ previewErrors[getPreviewKey(file, index)] }}
                      </div>
                    </template>
                    <pre v-else class="cli-preview-content">{{ file.content }}</pre>
                  </div>
                </TabPanel>
                <!-- Current Tab: 当前磁盘配置 -->
                <TabPanel class="cli-preview-list">
                  <div
                    v-for="(file, index) in currentFiles"
                    :key="'current-' + getCurrentKey(file, index)"
                    class="cli-preview-card"
                  >
                    <div class="cli-preview-meta">
                      <span class="cli-preview-name">{{ file.path || t('components.cliConfig.previewUnknownPath') }}</span>
                      <span class="cli-preview-format">{{ (file.format || config?.configFormat || '').toUpperCase() }}</span>
                    </div>
                    <template v-if="currentEditable">
                      <textarea
                        :ref="index === 0 ? (el) => currentTextareaRef = el as HTMLTextAreaElement : undefined"
                        v-model="currentEditingContent[getCurrentKey(file, index)]"
                        class="cli-preview-textarea"
                        rows="8"
                      />
                      <div class="cli-preview-actions">
                        <button
                          type="button"
                          class="cli-action-btn cli-primary-btn"
                          :disabled="currentSaving"
                          @click="handleApplyCurrentEdit(file, index)"
                        >
                          {{ t('components.cliConfig.previewApply') }}
                        </button>
                        <button
                          type="button"
                          class="cli-action-btn"
                          :disabled="currentSaving"
                          @click="handleResetCurrentEdit(file, index)"
                        >
                          {{ t('components.cliConfig.previewReset') }}
                        </button>
                      </div>
                      <div
                        v-if="currentErrors[getCurrentKey(file, index)]"
                        class="cli-preview-error"
                      >
                        {{ currentErrors[getCurrentKey(file, index)] }}
                      </div>
                    </template>
                    <pre v-else class="cli-preview-content">{{ file.content }}</pre>
                  </div>
                </TabPanel>
              </TabPanels>
            </TabGroup>
          </div>
        </div>
      </template>

      <div v-else class="cli-error">
        {{ t('components.cliConfig.loadError') }}
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { TabGroup, TabList, Tab, TabPanels, TabPanel } from '@headlessui/vue'
import {
  fetchCLIConfig,
  fetchCLIConfigSnapshots,
  saveCLIConfigFileContent,
  fetchCLITemplate,
  setCLITemplate,
  restoreDefaultConfig,
  type CLIPlatform,
  type CLIConfig,
  type CLIConfigField,
  type CLIConfigFile,
  type CLIConfigSnapshots,
} from '../../services/cliConfig'
import { showToast } from '../../utils/toast'
import { extractErrorMessage } from '../../utils/error'

const props = defineProps<{
  platform: CLIPlatform
  modelValue?: Record<string, any>
  // Gemini 供应商配置（用于预览"激活后"的 .env 内容）
  providerConfig?: {
    apiKey?: string
    baseUrl?: string
  }
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: Record<string, any>): void
}>()

const { t } = useI18n()

const expanded = ref(false)
const loading = ref(false)
const config = ref<CLIConfig | null>(null)
const editableValues = ref<Record<string, any>>({})
const isGlobalTemplate = ref(false)
type CustomField = { id: string; key: string; keyDraft: string; value: string }
const customFields = ref<CustomField[]>([])
let customFieldIdSeed = 0
const newCustomFieldId = () => `custom-field-${Date.now()}-${customFieldIdSeed++}`
const previewExpanded = ref(false)
const previewEditable = ref(false)
const previewSaving = ref(false)
const editingContent = ref<Record<string, string>>({})
const previewErrors = ref<Record<string, string>>({})
const firstTextareaRef = ref<HTMLTextAreaElement | null>(null)
const selectedPreviewTab = ref(0) // 0: Preview, 1: Current

// Current 标签页编辑状态
const currentEditable = ref(false)
const currentSaving = ref(false)
const currentEditingContent = ref<Record<string, string>>({})
const currentErrors = ref<Record<string, string>>({})
const currentTextareaRef = ref<HTMLTextAreaElement | null>(null)

// 配置快照数据（来自后端 GetConfigSnapshots API）
const snapshotsData = ref<CLIConfigSnapshots | null>(null)

// 防抖和请求序号保护（防止竞态条件）
let snapshotDebounceTimer: ReturnType<typeof setTimeout> | null = null
let snapshotRequestSeq = 0

// 获取所有预置字段的 key（包括锁定和可编辑）
const presetFieldKeys = computed(() => {
  const keys = new Set<string>()
  config.value?.fields.forEach(f => keys.add(f.key))
  return keys
})

// 获取所有锁定字段的 key
const lockedFieldKeys = computed(() => {
  const keys = new Set<string>()
  config.value?.fields.filter(f => f.locked).forEach(f => keys.add(f.key))
  return keys
})

const platformLabels: Record<CLIPlatform, string> = {
  claude: 'Claude Code',
  codex: 'Codex',
  gemini: 'Gemini',
}

const platformLabel = computed(() => platformLabels[props.platform] || props.platform)

// 检查是否有有效的供应商输入（用于决定 previewMode）
// - Claude/Codex: 需要同时有 apiKey 和 baseUrl 才视为有效（与真实 ApplySingleProvider 一致）
// - Gemini: 任一非空即可（Gemini 允许只配置其中一个）
const hasProviderInput = computed(() => {
  const apiKey = props.providerConfig?.apiKey?.trim() || ''
  const baseUrl = props.providerConfig?.baseUrl?.trim() || ''

  if (props.platform === 'gemini') {
    // Gemini 允许只配置 apiKey 或只配置 baseUrl
    return !!(apiKey || baseUrl)
  }
  // Claude/Codex 需要同时有 apiKey 和 baseUrl
  return !!(apiKey && baseUrl)
})

const lockedFields = computed(() => {
  const fields = config.value?.fields.filter(f => f.locked) || []

  // 仅当有有效输入时，用供应商配置值覆盖显示
  if (hasProviderInput.value) {
    // 提取并 trim 供应商配置值（避免 TS 窄化问题和显示不一致）
    const apiKey = props.providerConfig?.apiKey?.trim() || ''
    const baseUrl = props.providerConfig?.baseUrl?.trim() || ''

    return fields.map(field => {
      const newField = { ...field }

      if (props.platform === 'gemini') {
        if (field.key === 'GEMINI_API_KEY' && apiKey) {
          newField.value = apiKey
        }
        if (field.key === 'GOOGLE_GEMINI_BASE_URL' && baseUrl) {
          newField.value = baseUrl
        }
      }

      if (props.platform === 'claude') {
        if (field.key === 'env.ANTHROPIC_BASE_URL' && baseUrl) {
          newField.value = baseUrl
        }
        if (field.key === 'env.ANTHROPIC_AUTH_TOKEN' && apiKey) {
          newField.value = apiKey
        }
      }

      // Codex 平台：注入 base_url（config.toml）和 OPENAI_API_KEY（auth.json）
      if (props.platform === 'codex') {
        // config.toml 中的 base_url 字段（支持任意 providerKey）
        if (field.key.includes('.base_url') && baseUrl) {
          newField.value = baseUrl
        }
        // auth.json 中的 OPENAI_API_KEY 字段
        if (field.key === 'OPENAI_API_KEY' && apiKey) {
          newField.value = apiKey
        }
      }

      return newField
    })
  }

  return fields
})

const editableFields = computed(() => {
  return config.value?.fields.filter(f => !f.locked) || []
})

// 配置文件预览列表（来自后端 GetConfigSnapshots API）
const previewFiles = computed((): CLIConfigFile[] => {
  return snapshotsData.value?.previewFiles || []
})

// 当前磁盘状态（来自后端 GetConfigSnapshots API）
const currentFiles = computed((): CLIConfigFile[] => {
  return snapshotsData.value?.currentFiles || []
})

// 获取字段值，支持嵌套的 env.* 字段
const getFieldValue = (key: string) => {
  if (key.startsWith('env.')) {
    const envKey = key.slice(4)
    const env = editableValues.value.env as Record<string, any> | undefined
    return env ? env[envKey] : undefined
  }
  return editableValues.value[key]
}

const toggleExpanded = () => {
  expanded.value = !expanded.value
  if (expanded.value && !config.value) {
    loadConfig()
  }
}

// 加载配置快照（用于 Preview/Current 对比）
const loadSnapshots = async () => {
  // 递增请求序号，用于忽略过期响应
  const currentSeq = ++snapshotRequestSeq

  try {
    // 确定 previewMode：
    // - 有有效供应商输入 => "direct"（模拟直连应用）
    // - 无有效输入 => "current"（Preview = Current，不做任何注入）
    const previewMode = hasProviderInput.value ? 'direct' : 'current'
    const apiUrl = props.providerConfig?.baseUrl?.trim() || ''
    const apiKey = props.providerConfig?.apiKey?.trim() || ''

    const result = await fetchCLIConfigSnapshots(
      props.platform,
      apiUrl,
      apiKey,
      previewMode
    )

    // 忽略过期响应（有更新的请求已发出）
    if (currentSeq !== snapshotRequestSeq) {
      return
    }

    snapshotsData.value = result
  } catch (error) {
    // 忽略过期请求的错误
    if (currentSeq !== snapshotRequestSeq) {
      return
    }
    console.error('Failed to load CLI config snapshots:', error)
    snapshotsData.value = null
  }
}

// 防抖加载快照（用于 watch 触发，避免频繁请求）
const loadSnapshotsDebounced = () => {
  if (snapshotDebounceTimer) {
    clearTimeout(snapshotDebounceTimer)
  }
  snapshotDebounceTimer = setTimeout(() => {
    loadSnapshots()
  }, 300)
}

const loadConfig = async () => {
  loading.value = true
  try {
    config.value = await fetchCLIConfig(props.platform)
    editableValues.value = { ...(config.value?.editable || {}) }

    // 加载模板状态，并在新供应商时应用默认模板
    const template = await fetchCLITemplate(props.platform)
    isGlobalTemplate.value = template?.isGlobalDefault || false

    // 判断是否为新供应商（modelValue 为空或未定义）
    // 注意：editableValues 可能被后端填充了默认值，所以必须检查 modelValue
    const isNewProvider = !props.modelValue || Object.keys(props.modelValue).length === 0
    if (isNewProvider && template?.isGlobalDefault && template.template) {
      // 将模板值覆盖到当前可编辑值
      editableValues.value = { ...editableValues.value, ...template.template }
      emitChanges()
    }

    // 叠加外部传入的现有配置（含自定义字段），避免展开后被默认值覆盖
    if (props.modelValue && Object.keys(props.modelValue).length > 0) {
      editableValues.value = { ...editableValues.value, ...props.modelValue }
    }

    // 提取自定义字段（在预置字段列表加载后）
    extractCustomFields()

    // 加载配置快照（用于 Preview/Current 标签页）
    await loadSnapshots()

    // 初始化预览可编辑内容
    initPreviewEditing()
    // 重置 Current 编辑状态（切换平台/恢复默认时丢弃未保存编辑）
    currentEditable.value = false
    currentEditingContent.value = {}
    currentErrors.value = {}
  } catch (error) {
    console.error('Failed to load CLI config:', error)
    config.value = null
    showToast(t('components.cliConfig.loadError'), 'error')
  } finally {
    loading.value = false
  }
}

const updateField = (key: string, value: any) => {
  if (key.startsWith('env.')) {
    // 处理嵌套的 env.* 字段
    const envKey = key.slice(4)
    const env = { ...(editableValues.value.env as Record<string, any> || {}) }
    env[envKey] = value
    editableValues.value.env = env
  } else {
    editableValues.value[key] = value
  }
  emitChanges()
}

const updateFieldJSON = (key: string, jsonStr: string) => {
  try {
    const parsed = JSON.parse(jsonStr)
    editableValues.value[key] = parsed
    emitChanges()
  } catch {
    showToast(t('components.cliConfig.jsonParseError'), 'error')
  }
}

const emitChanges = () => {
  // 合并自定义字段到 editableValues
  const merged = { ...editableValues.value }

  // 清理 merged 中残留的旧自定义字段
  const activeCustomKeys = new Set(customFields.value.map(f => f.key.trim()).filter(k => k))

  Object.keys(merged).forEach(key => {
    // 如果该 key 不是预置/锁定字段，也不是对象，则视为自定义字段
    const isPotentialCustom = !presetFieldKeys.value.has(key) &&
                              !lockedFieldKeys.value.has(key) &&
                              typeof merged[key] !== 'object'

    // 如果它不在当前有效的自定义字段列表中，说明是残留的旧 key，应当清除
    if (isPotentialCustom && !activeCustomKeys.has(key)) {
      delete merged[key]
    }
  })

  customFields.value.forEach(field => {
    const key = field.key.trim()
    if (key) {
      merged[key] = field.value
    }
  })
  emit('update:modelValue', merged)
}

// ========== 自定义字段管理 ==========

const addCustomField = () => {
  customFields.value.push({ id: newCustomFieldId(), key: '', keyDraft: '', value: '' })
}

const removeCustomField = (index: number) => {
  const field = customFields.value[index]
  // 如果字段已有 key，从 editableValues 中删除
  if (field.key && editableValues.value[field.key] !== undefined) {
    delete editableValues.value[field.key]
  }
  customFields.value.splice(index, 1)
  emitChanges()
}

const updateCustomFieldKey = (index: number, newKey: string) => {
  customFields.value[index].keyDraft = newKey
}

const commitCustomFieldKey = (index: number) => {
  const field = customFields.value[index]
  const oldKey = field.key
  const normalizedKey = field.keyDraft.trim()

  // 未变化：只做 trim 同步
  if (normalizedKey === oldKey) {
    if (field.keyDraft !== normalizedKey) {
      field.keyDraft = normalizedKey
    }
    return
  }

  // 空 key：删除旧 key，但保留该行
  if (!normalizedKey) {
    if (oldKey && editableValues.value[oldKey] !== undefined) {
      delete editableValues.value[oldKey]
    }
    field.key = ''
    field.keyDraft = ''
    emitChanges()
    return
  }

  // 只在提交时校验
  if (lockedFieldKeys.value.has(normalizedKey)) {
    showToast(t('components.cliConfig.keyConflictLocked'), 'error')
    field.keyDraft = oldKey
    return
  }
  if (presetFieldKeys.value.has(normalizedKey)) {
    showToast(t('components.cliConfig.keyConflictPreset'), 'error')
    field.keyDraft = oldKey
    return
  }
  const duplicate = customFields.value.some((f, i) => i !== index && f.key === normalizedKey)
  if (duplicate) {
    showToast(t('components.cliConfig.keyDuplicate'), 'error')
    field.keyDraft = oldKey
    return
  }

  if (oldKey && editableValues.value[oldKey] !== undefined) {
    delete editableValues.value[oldKey]
  }

  field.key = normalizedKey
  field.keyDraft = normalizedKey
  emitChanges()
}

const updateCustomFieldValue = (index: number, value: string) => {
  const field = customFields.value[index]
  field.value = value
  // key 为空表示仍是未提交的草稿行：不向上游同步，避免触发 watch→extract 导致行丢失
  if (!field.key.trim()) {
    return
  }
  emitChanges()
}

// 从 editableValues 中提取自定义字段（不在预置列表中的）
const extractCustomFields = () => {
  const existing = customFields.value.slice()

  // 1) 复用已存在字段的 id（按已提交 key 映射）
  const existingByKey = new Map<string, CustomField>()
  existing.forEach((field) => {
    const key = field.key.trim()
    if (key && !existingByKey.has(key)) {
      existingByKey.set(key, field)
    }
  })

  // 2) 保留空 key 的草稿行（避免 blur 清空后被 watch→extract 吃掉）
  const draftRows = existing.filter((field) => !field.key.trim())

  // 3) 从 editableValues 中提取自定义字段 key
  const extractedKeys: string[] = []
  for (const key in editableValues.value) {
    if (!key) continue
    const val = editableValues.value[key]
    // 跳过预置/锁定字段和嵌套对象（如 env）；允许 null 值作为普通值
    const isObjectLike = typeof val === 'object' && val !== null
    if (!presetFieldKeys.value.has(key) && !lockedFieldKeys.value.has(key) && !isObjectLike) {
      extractedKeys.push(key)
    }
  }

  const remaining = new Set(extractedKeys)
  const custom: CustomField[] = []

  // 4) 先按现有顺序保留仍存在的字段，确保顺序与 id 稳定
  existing.forEach((field) => {
    const key = field.key.trim()
    if (!key) return
    if (!remaining.has(key)) return
    custom.push({
      ...field,
      value: String(editableValues.value[key]),
    })
    remaining.delete(key)
  })

  // 5) 再追加新增字段
  remaining.forEach((key) => {
    const reused = existingByKey.get(key)
    if (reused) {
      custom.push({
        ...reused,
        value: String(editableValues.value[key]),
      })
      return
    }
    custom.push({
      id: newCustomFieldId(),
      key,
      keyDraft: key,
      value: String(editableValues.value[key]),
    })
  })

  // 6) 最后追加空 key 草稿行
  draftRows.forEach((row) => custom.push(row))

  customFields.value = custom
}

const handleTemplateChange = async () => {
  try {
    // 无论是启用还是禁用模板，都保存状态
    await setCLITemplate(props.platform, editableValues.value, isGlobalTemplate.value)
    showToast(t('components.cliConfig.templateSaved'), 'success')
  } catch (error) {
    console.error('Failed to save template:', error)
    showToast(t('components.cliConfig.templateSaveError'), 'error')
    // 恢复原来的状态
    isGlobalTemplate.value = !isGlobalTemplate.value
  }
}

const handleRestoreDefault = async () => {
  if (!confirm(t('components.cliConfig.restoreConfirm'))) {
    return
  }

  try {
    await restoreDefaultConfig(props.platform)
    await loadConfig()
    showToast(t('components.cliConfig.restoreSuccess'), 'success')
  } catch (error) {
    console.error('Failed to restore default:', error)
    showToast(t('components.cliConfig.restoreError'), 'error')
  }
}

// ========== 智能粘贴功能 ==========

const handleSmartPaste = (event: ClipboardEvent) => {
  // 如果在输入框内粘贴，不触发智能解析
  const target = event.target as HTMLElement
  if (
    target.tagName === 'INPUT' ||
    target.tagName === 'TEXTAREA' ||
    target.tagName === 'SELECT' ||
    target.isContentEditable
  ) {
    return
  }

  const text = event.clipboardData?.getData('text')?.trim()
  if (!text) return

  const parsed = parseSmartConfig(text)
  if (!parsed) {
    // 只有看起来像配置的内容才提示错误
    if (text.includes('{') || text.includes('=') || text.includes('\n')) {
      showToast(t('components.cliConfig.smartPasteFailed'), 'error')
    }
    return
  }

  event.preventDefault()
  applyParsedConfig(parsed.data)
  showToast(t('components.cliConfig.smartPasteSuccess', { format: parsed.format.toUpperCase() }), 'success')
}

const parseSmartConfig = (content: string): { data: Record<string, any>; format: 'json' | 'toml' | 'env' } | null => {
  // 尝试 JSON
  try {
    const jsonVal = JSON.parse(content)
    if (jsonVal && typeof jsonVal === 'object') {
      return { data: jsonVal as Record<string, any>, format: 'json' }
    }
  } catch {
    // ignore
  }

  // 尝试 TOML（轻量解析）
  const tomlVal = parseTomlLite(content)
  if (tomlVal) {
    return { data: tomlVal, format: 'toml' }
  }

  // 尝试 ENV
  const envVal = parseEnvText(content)
  if (envVal && Object.keys(envVal).length > 0) {
    return { data: envVal, format: 'env' }
  }

  return null
}

// 轻量 TOML 解析，仅支持键值行
const parseTomlLite = (content: string): Record<string, any> | null => {
  const result: Record<string, any> = {}
  const lines = content.split(/\r?\n/)
  lines.forEach(line => {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#') || trimmed.startsWith('[')) return
    const eqIndex = trimmed.indexOf('=')
    if (eqIndex === -1) return
    const key = trimmed.slice(0, eqIndex).trim()
    let value: any = trimmed.slice(eqIndex + 1).trim()
    if (!key) return
    if (value.startsWith('"') && value.endsWith('"')) {
      value = value.slice(1, -1)
    } else if (/^(true|false)$/i.test(value)) {
      value = value.toLowerCase() === 'true'
    } else if (!Number.isNaN(Number(value)) && value !== '') {
      value = Number(value)
    }
    result[key] = value
  })
  return Object.keys(result).length > 0 ? result : null
}

// 解析 ENV 格式
const parseEnvText = (content: string): Record<string, string> => {
  const result: Record<string, string> = {}
  const lines = content.split(/\r?\n/)
  lines.forEach(line => {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) return
    const eqIndex = trimmed.indexOf('=')
    if (eqIndex === -1) return
    const key = trimmed.slice(0, eqIndex).trim()
    const value = trimmed.slice(eqIndex + 1).trim()
    if (key) {
      result[key] = value
    }
  })
  return result
}

// 应用解析后的配置
const applyParsedConfig = (data: Record<string, any>) => {
  const next = { ...editableValues.value }

  const mergeCustom = (key: string, value: any) => {
    next[key] = value
  }

  const mergeEnv = (envData: Record<string, any>, locked: string[] = []) => {
    const env = { ...(next.env as Record<string, any> || {}) }
    Object.entries(envData).forEach(([k, v]) => {
      if (!locked.includes(k)) {
        env[k] = v
      }
    })
    next.env = env
  }

  const coerceBoolean = (value: any): boolean | undefined => {
    if (typeof value === 'boolean') return value
    if (typeof value === 'string') {
      const lowered = value.trim().toLowerCase()
      if (lowered === 'true') return true
      if (lowered === 'false') return false
    }
    return undefined
  }

  switch (props.platform) {
    case 'claude': {
      if (typeof data.model === 'string') next.model = data.model
      if (typeof data.alwaysThinkingEnabled !== 'undefined') {
        const boolVal = coerceBoolean(data.alwaysThinkingEnabled)
        if (typeof boolVal === 'boolean') {
          next.alwaysThinkingEnabled = boolVal
        }
      }
      if (data.enabledPlugins && typeof data.enabledPlugins === 'object') {
        next.enabledPlugins = data.enabledPlugins
      }
      if (data.env && typeof data.env === 'object') {
        mergeEnv(data.env as Record<string, any>, ['ANTHROPIC_BASE_URL', 'ANTHROPIC_AUTH_TOKEN'])
      } else {
        const envCandidates: Record<string, any> = {}
        Object.entries(data).forEach(([k, v]) => {
          if (/^[A-Z0-9_]+$/.test(k)) {
            envCandidates[k] = v
          }
        })
        if (Object.keys(envCandidates).length) {
          mergeEnv(envCandidates, ['ANTHROPIC_BASE_URL', 'ANTHROPIC_AUTH_TOKEN'])
        }
      }
      break
    }
    case 'codex': {
      if (typeof data.model === 'string') next.model = data.model
      if (typeof data.model_reasoning_effort === 'string') next.model_reasoning_effort = data.model_reasoning_effort
      if (typeof data.disable_response_storage !== 'undefined') {
        const boolVal = coerceBoolean(data.disable_response_storage)
        if (typeof boolVal === 'boolean') {
          next.disable_response_storage = boolVal
        }
      }
      break
    }
    case 'gemini': {
      Object.entries(data).forEach(([k, v]) => {
        if (k === 'GEMINI_API_KEY' && typeof v === 'string') {
          next.GEMINI_API_KEY = v
        } else if (k === 'GEMINI_MODEL' && typeof v === 'string') {
          next.GEMINI_MODEL = v
        } else if (/^[A-Z0-9_]+$/.test(k) && k !== 'GOOGLE_GEMINI_BASE_URL') {
          mergeCustom(k, v)
        }
      })
      break
    }
    default:
      break
  }

  // 兜底：将未匹配的普通键作为自定义字段
  Object.entries(data).forEach(([k, v]) => {
    if (!presetFieldKeys.value.has(k) && !lockedFieldKeys.value.has(k) && typeof v !== 'object') {
      mergeCustom(k, v)
    }
  })

  editableValues.value = next
  extractCustomFields()
  emitChanges()
}

// 切换预览区展开状态
const togglePreview = () => {
  previewExpanded.value = !previewExpanded.value
}

// 切换预览区编辑模式
const togglePreviewEditable = () => {
  previewEditable.value = !previewEditable.value
  if (!previewEditable.value) {
    // 关闭编辑模式时清理错误
    previewErrors.value = {}
  } else {
    // 解锁编辑模式时
    if (Object.keys(editingContent.value).length === 0) {
      // 首次解锁时，如果还没初始化，补一次
      initPreviewEditing()
    }
    // 等待 DOM 更新后聚焦第一个 textarea（修复 macOS WebView 键盘输入问题）
    nextTick(() => {
      firstTextareaRef.value?.focus()
    })
  }
}

// 生成预览文件的唯一 key
const getPreviewKey = (file: CLIConfigFile, index: number): string => {
  // 优先使用 path，否则使用 format-index 组合确保唯一性
  return file.path || `${file.format || 'file'}-${index}`
}

// 初始化预览编辑内容
const initPreviewEditing = () => {
  const nextContent: Record<string, string> = {}
  previewFiles.value.forEach((file, index) => {
    const key = getPreviewKey(file, index)
    nextContent[key] = file.content || ''
  })
  editingContent.value = nextContent
  previewErrors.value = {}
}

// 应用预览编辑
const handleApplyPreviewEdit = async (file: CLIConfigFile, index: number) => {
  const key = getPreviewKey(file, index)
  const text = editingContent.value[key] ?? file.content ?? ''
  // 缓存当前平台，防止保存/刷新过程中切换平台导致竞态
  const platform = props.platform

  if (!file.path) {
    previewErrors.value[key] = t('components.cliConfig.previewUnknownPath')
    showToast(t('components.cliConfig.previewUnknownPath'), 'error')
    return
  }

  // 防御：避免极端情况下的重复触发（双击/连点）
  if (previewSaving.value) return

  previewSaving.value = true
  try {
    await saveCLIConfigFileContent(platform, file.path, text)
    // 校验平台是否在保存过程中发生变化
    if (platform !== props.platform) {
      console.warn('[CLIConfigEditor] Platform changed during save, skipping state update')
      return
    }
    // 重新拉取，让预览展示真实落盘内容（含后端强制写入的锁定字段）
    const nextConfig = await fetchCLIConfig(platform)
    // 校验平台是否在刷新过程中发生变化（避免旧平台结果覆盖新平台界面状态）
    if (platform !== props.platform) {
      console.warn('[CLIConfigEditor] Platform changed during fetch, skipping state update')
      return
    }
    config.value = nextConfig
    // 同步 editableValues 到新配置，避免表单状态不一致
    editableValues.value = { ...(nextConfig.editable || {}) }
    // 提取自定义字段（防止预览保存覆盖了自定义字段后表单丢失）
    extractCustomFields()
    // 通知父组件（避免后续表单提交覆盖预览保存的内容）
    emitChanges()
    // 刷新快照数据以更新 Preview/Current 标签页
    await loadSnapshots()
    // 再次校验平台（loadSnapshots 是异步操作）
    if (platform !== props.platform) {
      console.warn('[CLIConfigEditor] Platform changed during snapshot refresh, skipping state update')
      return
    }
    // 仅重置当前文件的预览内容，保留其他文件的未保存编辑
    editingContent.value[key] = previewFiles.value.find((f, i) => getPreviewKey(f, i) === key)?.content || ''
    delete previewErrors.value[key]
    showToast(t('components.cliConfig.previewApplySuccess'), 'success')
  } catch (error) {
    console.error('Failed to save preview content:', error)
    const errorMsg = extractErrorMessage(error, t('components.cliConfig.loadError'))
    previewErrors.value[key] = errorMsg
    showToast(errorMsg, 'error')
  } finally {
    previewSaving.value = false
  }
}

// 还原预览编辑
const handleResetPreviewEdit = (file: CLIConfigFile, index: number) => {
  const key = getPreviewKey(file, index)
  editingContent.value[key] = file.content || ''
  delete previewErrors.value[key]
}

// ========== Current 标签页编辑函数 ==========

// 生成 Current 文件的唯一 key（与 getPreviewKey 保持一致，前缀在 DOM :key 处添加）
const getCurrentKey = (file: CLIConfigFile, index: number): string => {
  return file.path || `${file.format || 'file'}-${index}`
}

// 切换 Current 区编辑模式
const toggleCurrentEditable = () => {
  currentEditable.value = !currentEditable.value
  if (!currentEditable.value) {
    // 锁定时清空编辑缓冲，避免旧数据意外复用
    currentEditingContent.value = {}
    currentErrors.value = {}
  } else {
    // 解锁时总是从最新磁盘内容初始化（Current 语义是实时磁盘状态）
    initCurrentEditing()
    nextTick(() => {
      currentTextareaRef.value?.focus()
    })
  }
}

// 初始化 Current 编辑内容
const initCurrentEditing = () => {
  const nextContent: Record<string, string> = {}
  currentFiles.value.forEach((file, index) => {
    const key = getCurrentKey(file, index)
    nextContent[key] = file.content || ''
  })
  currentEditingContent.value = nextContent
  currentErrors.value = {}
}

// 应用 Current 编辑（保存到磁盘）
const handleApplyCurrentEdit = async (file: CLIConfigFile, index: number) => {
  const key = getCurrentKey(file, index)
  const text = currentEditingContent.value[key] ?? file.content ?? ''
  // 缓存当前平台，防止保存过程中切换平台导致竞态
  const platform = props.platform
  // 保存文件路径，用于后续从 nextConfig 中精确查找最新内容
  const targetPath = file.path

  if (!targetPath) {
    currentErrors.value[key] = t('components.cliConfig.previewUnknownPath')
    showToast(t('components.cliConfig.previewUnknownPath'), 'error')
    return
  }

  // 防御：避免极端情况下的重复触发（双击/连点）
  if (currentSaving.value) return

  currentSaving.value = true
  try {
    await saveCLIConfigFileContent(platform, targetPath, text)
    // 校验平台是否在保存过程中发生变化
    if (platform !== props.platform) {
      console.warn('[CLIConfigEditor] Platform changed during save, skipping state update')
      return
    }
    // 重新拉取配置以同步状态
    const nextConfig = await fetchCLIConfig(platform)
    // 校验平台是否在刷新过程中发生变化（避免旧平台结果覆盖新平台界面状态）
    if (platform !== props.platform) {
      console.warn('[CLIConfigEditor] Platform changed during fetch, skipping state update')
      return
    }
    config.value = nextConfig
    editableValues.value = { ...(nextConfig.editable || {}) }
    extractCustomFields()
    emitChanges()
    // 刷新快照数据以更新 Preview/Current 标签页
    await loadSnapshots()
    // 再次校验平台（loadSnapshots 是异步操作）
    if (platform !== props.platform) {
      console.warn('[CLIConfigEditor] Platform changed during snapshot refresh, skipping state update')
      return
    }

    // 从刷新后的 currentFiles 获取最新内容
    const refreshedFile = currentFiles.value.find((f, i) => getCurrentKey(f, i) === key)
    currentEditingContent.value[key] = refreshedFile?.content || ''
    delete currentErrors.value[key]
    showToast(t('components.cliConfig.previewApplySuccess'), 'success')
  } catch (error) {
    console.error('Failed to save current file content:', error)
    const errorMsg = extractErrorMessage(error, t('components.cliConfig.loadError'))
    currentErrors.value[key] = errorMsg
    showToast(errorMsg, 'error')
  } finally {
    currentSaving.value = false
  }
}

// 还原 Current 编辑
const handleResetCurrentEdit = (file: CLIConfigFile, index: number) => {
  const key = getCurrentKey(file, index)
  currentEditingContent.value[key] = file.content || ''
  delete currentErrors.value[key]
}

// 监听 modelValue 变化
watch(() => props.modelValue, (newVal) => {
  if (newVal && Object.keys(newVal).length > 0) {
    editableValues.value = { ...newVal }
    if (config.value) {
      extractCustomFields()
    }
  } else {
    editableValues.value = {}
    customFields.value = []
  }
}, { immediate: true })

// 监听平台变化
watch(() => props.platform, () => {
  if (expanded.value) {
    loadConfig()
  } else {
    config.value = null
  }
})

// 监听供应商配置变化，重新加载快照（用于实时更新预览）
// 使用防抖避免频繁请求，使用请求序号避免竞态条件
watch(
  () => [props.providerConfig?.apiKey, props.providerConfig?.baseUrl],
  () => {
    // 仅在组件展开且配置已加载时重新加载快照
    if (expanded.value && config.value) {
      loadSnapshotsDebounced()
    }
  },
  { deep: true }
)

onMounted(() => {
  // 如果有初始值，自动展开
  if (props.modelValue && Object.keys(props.modelValue).length > 0) {
    expanded.value = true
    loadConfig()
  }
})

onUnmounted(() => {
  // 清理防抖定时器，避免组件卸载后仍有请求发出
  if (snapshotDebounceTimer) {
    clearTimeout(snapshotDebounceTimer)
    snapshotDebounceTimer = null
  }
})
</script>

<style scoped>
.cli-config-editor {
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  overflow: hidden;
  margin-top: 16px;
}

.cli-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background: var(--mac-surface);
  cursor: pointer;
  user-select: none;
  transition: background 0.2s;
}

.cli-header:hover {
  background: var(--mac-surface-strong);
}

.cli-header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.expand-icon {
  width: 16px;
  height: 16px;
  transition: transform 0.2s;
  opacity: 0.6;
}

.expand-icon.expanded {
  transform: rotate(180deg);
}

.cli-title {
  font-size: 14px;
  font-weight: 500;
  color: var(--mac-text);
}

.cli-platform-badge {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--mac-accent);
  color: white;
  font-weight: 500;
}

.cli-header-right {
  display: flex;
  gap: 8px;
}

.cli-action-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  border-radius: 6px;
  background: transparent;
  cursor: pointer;
  transition: background 0.2s;
}

.cli-action-btn:hover {
  background: var(--mac-surface-strong);
}

.cli-action-btn svg {
  width: 16px;
  height: 16px;
  color: var(--mac-text-secondary);
}

.cli-content {
  padding: 16px;
  border-top: 1px solid var(--mac-border);
  background: var(--mac-surface);
}

.cli-loading {
  text-align: center;
  padding: 24px;
  color: var(--mac-text-secondary);
  font-size: 14px;
}

.cli-error {
  text-align: center;
  padding: 24px;
  color: var(--mac-error);
  font-size: 14px;
}

.cli-section {
  margin-bottom: 20px;
}

.cli-section:last-child {
  margin-bottom: 0;
}

.cli-section-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--mac-text-secondary);
  margin-bottom: 12px;
}

.lock-icon,
.edit-icon,
.custom-icon {
  font-size: 14px;
}

.cli-fields {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.cli-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cli-field-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--mac-text);
  font-family: monospace;
}

.cli-field-input {
  padding: 8px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  font-size: 13px;
  background: var(--mac-bg);
  color: var(--mac-text);
  transition: border-color 0.2s;
}

.cli-field-input:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.cli-field-input.disabled {
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
  cursor: not-allowed;
}

.cli-field-textarea {
  padding: 8px 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  font-size: 12px;
  font-family: monospace;
  background: var(--mac-bg);
  color: var(--mac-text);
  resize: vertical;
  min-height: 60px;
}

.cli-field-textarea:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.cli-field-hint {
  font-size: 11px;
  color: var(--mac-text-tertiary);
}

.cli-add-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  margin-left: auto;
  border: none;
  border-radius: 4px;
  background: var(--mac-accent);
  cursor: pointer;
  transition: opacity 0.2s;
}

.cli-add-btn:hover {
  opacity: 0.8;
}

.cli-add-btn svg {
  width: 14px;
  height: 14px;
  color: white;
}

.cli-custom-field {
  display: flex;
  align-items: center;
  gap: 8px;
}

.cli-key-input {
  flex: 1;
  min-width: 0;
}

.cli-value-input {
  flex: 2;
  min-width: 0;
}

.cli-delete-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  border: none;
  border-radius: 4px;
  background: transparent;
  cursor: pointer;
  transition: background 0.2s;
}

.cli-delete-btn:hover {
  background: var(--mac-error-bg, rgba(255, 59, 48, 0.1));
}

.cli-delete-btn svg {
  width: 14px;
  height: 14px;
  color: var(--mac-error, #ff3b30);
}

.cli-empty-hint {
  font-size: 13px;
  color: var(--mac-text-tertiary);
  padding: 12px;
  text-align: center;
  background: var(--mac-surface-strong);
  border-radius: 6px;
}

.cli-switch {
  position: relative;
  display: inline-block;
  width: 40px;
  height: 22px;
}

.cli-switch input {
  opacity: 0;
  width: 0;
  height: 0;
}

.cli-switch-slider {
  position: absolute;
  cursor: pointer;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background-color: var(--mac-border);
  border-radius: 22px;
  transition: 0.2s;
}

.cli-switch-slider:before {
  position: absolute;
  content: "";
  height: 18px;
  width: 18px;
  left: 2px;
  bottom: 2px;
  background-color: white;
  border-radius: 50%;
  transition: 0.2s;
}

.cli-switch input:checked + .cli-switch-slider {
  background-color: var(--mac-accent);
}

.cli-switch input:checked + .cli-switch-slider:before {
  transform: translateX(18px);
}

.cli-template-options {
  padding-top: 16px;
  border-top: 1px solid var(--mac-border);
  margin-top: 16px;
}

.cli-checkbox {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--mac-text);
  cursor: pointer;
}

.cli-checkbox input {
  width: 16px;
  height: 16px;
  cursor: pointer;
}

/* 预览区样式 */
.cli-preview-section {
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--mac-border);
}

.cli-preview-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 500;
  color: var(--mac-text-secondary);
  cursor: pointer;
  user-select: none;
  padding: 4px 0;
}

.cli-preview-header:hover {
  color: var(--mac-text);
}

.preview-icon {
  font-size: 14px;
}

/* Tabs 样式 */
.cli-preview-tabs-wrapper {
  margin-top: 12px;
}

.cli-tabs-list {
  display: flex;
  gap: 4px;
  padding: 4px;
  background: var(--mac-surface-strong);
  border-radius: 8px;
  margin-bottom: 12px;
}

.cli-tab-btn {
  flex: 1;
  padding: 6px 12px;
  font-size: 12px;
  font-weight: 500;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--mac-text-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
}

.cli-tab-btn:hover:not(.selected) {
  background: var(--mac-surface);
  color: var(--mac-text);
}

.cli-tab-btn.selected {
  background: var(--mac-accent);
  color: white;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

:global(.dark) .cli-tab-btn.selected {
  background: var(--mac-accent);
}

.cli-preview-count {
  margin-left: auto;
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 10px;
  background: var(--mac-surface-strong);
  color: var(--mac-text-secondary);
}

.cli-preview-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 12px;
}

.cli-preview-card {
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  background: var(--mac-surface-strong);
  overflow: hidden;
}

.cli-preview-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: var(--mac-surface);
  border-bottom: 1px solid var(--mac-border);
}

.cli-preview-name {
  font-size: 11px;
  color: var(--mac-text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
}

.cli-preview-format {
  font-size: 10px;
  font-weight: 600;
  padding: 2px 6px;
  border-radius: 4px;
  background: var(--mac-accent);
  color: white;
  flex-shrink: 0;
}

.cli-preview-content {
  margin: 0;
  padding: 12px;
  font-size: 11px;
  line-height: 1.5;
  max-height: 200px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  font-family: monospace;
  color: var(--mac-text);
  background: var(--mac-bg);
}

/* 预览区解锁编辑样式 */
.cli-preview-lock {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--mac-text-secondary);
  padding: 4px 8px;
}

.cli-preview-lock:hover {
  color: var(--mac-text);
}

.cli-preview-textarea {
  width: 100%;
  min-height: 160px;
  padding: 12px;
  border: 1px solid var(--mac-border);
  border-radius: 6px;
  font-size: 11px;
  line-height: 1.5;
  font-family: monospace;
  background: var(--mac-bg);
  color: var(--mac-text);
  resize: vertical;
}

.cli-preview-textarea:focus {
  outline: none;
  border-color: var(--mac-accent);
}

.cli-preview-actions {
  display: flex;
  gap: 8px;
  margin: 8px 12px 4px;
}

.cli-primary-btn {
  background: var(--mac-accent);
  color: white;
  padding: 6px 12px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
}

.cli-primary-btn:hover {
  opacity: 0.9;
}

.cli-preview-error {
  font-size: 12px;
  color: var(--mac-error, #ff3b30);
  margin: 4px 12px 8px;
}

/* 深色模式适配 */
:global(.dark) .cli-field-input {
  background: var(--mac-surface-strong);
}

:global(.dark) .cli-field-textarea {
  background: var(--mac-surface-strong);
}

:global(.dark) .cli-field-input.disabled {
  background: var(--mac-bg);
}

:global(.dark) .cli-preview-textarea {
  background: var(--mac-surface-strong);
}
</style>
