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
              :key="index"
              class="cli-custom-field"
            >
              <input
                type="text"
                :value="field.key"
                class="cli-field-input cli-key-input"
                :placeholder="t('components.cliConfig.keyPlaceholder')"
                @input="updateCustomFieldKey(index, ($event.target as HTMLInputElement).value)"
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
        <div v-if="previewFiles.length" class="cli-preview-section">
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
              v-if="previewExpanded"
              type="button"
              class="cli-action-btn cli-preview-lock"
              @click.stop="togglePreviewEditable"
            >
              <span v-if="previewEditable">🔓 {{ t('components.cliConfig.previewEditUnlocked') }}</span>
              <span v-else>🔒 {{ t('components.cliConfig.previewEditLocked') }}</span>
            </button>
          </div>
          <div v-if="previewExpanded" class="cli-preview-list">
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
                  v-model="editingContent[getPreviewKey(file, index)]"
                  class="cli-preview-textarea"
                  rows="8"
                />
                <div class="cli-preview-actions">
                  <button
                    type="button"
                    class="cli-action-btn cli-primary-btn"
                    @click="handleApplyPreviewEdit(file, index)"
                  >
                    {{ t('components.cliConfig.previewApply') }}
                  </button>
                  <button
                    type="button"
                    class="cli-action-btn"
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
import { ref, computed, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  fetchCLIConfig,
  fetchCLITemplate,
  setCLITemplate,
  restoreDefaultConfig,
  type CLIPlatform,
  type CLIConfig,
  type CLIConfigField,
  type CLIConfigFile,
} from '../../services/cliConfig'
import { showToast } from '../../utils/toast'

const props = defineProps<{
  platform: CLIPlatform
  modelValue?: Record<string, any>
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
const customFields = ref<Array<{ key: string; value: string }>>([])
const previewExpanded = ref(false)
const previewEditable = ref(false)
const editingContent = ref<Record<string, string>>({})
const previewErrors = ref<Record<string, string>>({})

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

const lockedFields = computed(() => {
  return config.value?.fields.filter(f => f.locked) || []
})

const editableFields = computed(() => {
  return config.value?.fields.filter(f => !f.locked) || []
})

// 配置文件预览列表
const previewFiles = computed((): CLIConfigFile[] => {
  if (!config.value) return []
  if (config.value.rawFiles && config.value.rawFiles.length > 0) {
    return config.value.rawFiles
  }
  // 回退兼容：使用 rawContent
  if (config.value.rawContent) {
    return [{
      path: config.value.filePath || '',
      format: config.value.configFormat,
      content: config.value.rawContent,
    }]
  }
  return []
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
    // 初始化预览可编辑内容
    initPreviewEditing()
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
  customFields.value.push({ key: '', value: '' })
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
  const field = customFields.value[index]
  const oldKey = field.key
  const normalizedKey = newKey.trim()

  // 空 key 直接清空并同步
  if (!normalizedKey) {
    field.key = ''
    emitChanges()
    return
  }

  // 检查是否与锁定字段冲突
  if (lockedFieldKeys.value.has(normalizedKey)) {
    showToast(t('components.cliConfig.keyConflictLocked'), 'error')
    return
  }

  // 检查是否与预置字段冲突
  if (presetFieldKeys.value.has(normalizedKey)) {
    showToast(t('components.cliConfig.keyConflictPreset'), 'error')
    return
  }

  // 检查是否与其他自定义字段重复
  const duplicate = customFields.value.some((f, i) => i !== index && f.key === normalizedKey)
  if (duplicate) {
    showToast(t('components.cliConfig.keyDuplicate'), 'error')
    return
  }

  // 如果旧 key 存在，从 editableValues 中删除
  if (oldKey && editableValues.value[oldKey] !== undefined) {
    delete editableValues.value[oldKey]
  }

  field.key = normalizedKey
  emitChanges()
}

const updateCustomFieldValue = (index: number, value: string) => {
  customFields.value[index].value = value
  emitChanges()
}

// 从 editableValues 中提取自定义字段（不在预置列表中的）
const extractCustomFields = () => {
  const custom: Array<{ key: string; value: string }> = []
  for (const key in editableValues.value) {
    // 跳过预置字段和嵌套对象（如 env）
    if (!presetFieldKeys.value.has(key) && typeof editableValues.value[key] !== 'object') {
      custom.push({ key, value: String(editableValues.value[key]) })
    }
  }
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
  } else if (Object.keys(editingContent.value).length === 0) {
    // 首次解锁时，如果还没初始化，补一次
    initPreviewEditing()
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
const handleApplyPreviewEdit = (file: CLIConfigFile, index: number) => {
  const key = getPreviewKey(file, index)
  const text = editingContent.value[key] ?? file.content ?? ''

  const parsed = parseSmartConfig(text)
  if (!parsed || Object.keys(parsed.data).length === 0) {
    previewErrors.value[key] = t('components.cliConfig.previewParseError')
    showToast(t('components.cliConfig.previewParseError'), 'error')
    return
  }

  applyParsedConfig(parsed.data)
  delete previewErrors.value[key]
  showToast(t('components.cliConfig.previewApplySuccess'), 'success')
}

// 还原预览编辑
const handleResetPreviewEdit = (file: CLIConfigFile, index: number) => {
  const key = getPreviewKey(file, index)
  editingContent.value[key] = file.content || ''
  delete previewErrors.value[key]
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

onMounted(() => {
  // 如果有初始值，自动展开
  if (props.modelValue && Object.keys(props.modelValue).length > 0) {
    expanded.value = true
    loadConfig()
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
