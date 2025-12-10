<template>
  <div class="main-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ t('components.mcp.hero.eyebrow') }}</p>
      <button class="ghost-icon" :aria-label="t('components.mcp.controls.back')" @click="goHome">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M15 18l-6-6 6-6"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
          />
        </svg>
      </button>
      <button class="ghost-icon" :aria-label="t('components.mcp.controls.settings')" @click="goToSettings">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M12 15a3 3 0 100-6 3 3 0 000 6z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
          <path
            d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"
            stroke="currentColor"
            stroke-width="1.5"
            stroke-linecap="round"
            stroke-linejoin="round"
            fill="none"
          />
        </svg>
      </button>
    </div>

    <div class="contrib-page">
      <section class="contrib-hero">
        <h1>{{ t('components.mcp.hero.title') }}</h1>
        <p class="lead">{{ t('components.mcp.hero.lead') }}</p>
      </section>

      <section class="automation-section">
        <div class="section-header section-header-solo">
          <div class="section-controls">
            <button
              class="ghost-icon"
              :aria-label="t('components.mcp.controls.refresh')"
              :disabled="loading"
              @click="reload"
            >
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M20.5 8a8.5 8.5 0 10-2.38 7.41"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
                <path
                  d="M20.5 4v4h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </svg>
            </button>
            <button class="ghost-icon" :aria-label="t('components.mcp.controls.create')" @click="openCreateModal">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M12 5v14M5 12h14"
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

        <div v-if="errorMessage" class="alert-error">{{ errorMessage }}</div>

        <div v-if="loading" class="empty-state">{{ t('components.mcp.list.loading') }}</div>

        <div v-else-if="!servers.length" class="empty-state">
          <p>{{ t('components.mcp.list.empty') }}</p>
          <BaseButton type="button" @click="openCreateModal">
            {{ t('components.mcp.controls.create') }}
          </BaseButton>
        </div>

        <div v-else class="automation-list">
          <article v-for="server in servers" :key="server.name" class="automation-card">
            <div class="card-leading">
              <div class="card-icon" :style="iconStyle(server.name)">
                <span v-if="iconSvg(server.name)" class="icon-svg" v-html="iconSvg(server.name)" aria-hidden="true"></span>
                <span v-else class="icon-fallback">{{ serverInitials(server.name) }}</span>
              </div>
              <div class="card-text">
                <div class="card-title-row">
                  <p class="card-title">{{ server.name }}</p>
                  <span class="chip">{{ typeLabel(server.type) }}</span>
                </div>
                <p class="card-metrics">{{ serverSummary(server) }}</p>
                <p v-if="server.website" class="card-link">
                  <a :href="server.website" target="_blank" rel="noreferrer">{{ server.website }}</a>
                </p>
                <p v-if="server.tips" class="card-tip">{{ server.tips }}</p>
              </div>
            </div>
            <div class="card-platforms">
              <div v-for="option in platformOptions" :key="option.id" class="platform-row">
                <div class="platform-info">
                  <span class="platform-label">{{ option.label }}</span>
                  <div class="platform-controls">
                    <span
                      class="platform-status"
                      :class="{ active: platformActive(server, option.id) }"
                    >
                      {{ platformActive(server, option.id) ? t('components.mcp.status.active') : t('components.mcp.status.inactive') }}
                    </span>
                    <label class="mac-switch sm">
                      <input
                        type="checkbox"
                        :checked="platformEnabled(server, option.id)"
                        :disabled="saveBusy"
                        @change="onPlatformToggle(server, option.id, $event)"
                      />
                      <span></span>
                    </label>
                  </div>
                </div>
              </div>
            </div>
            <div class="card-actions">
              <button class="ghost-icon" :aria-label="t('components.mcp.list.edit')" @click="openEditModal(server)">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path
                    d="M16.474 5.408l2.118 2.117m-.756-3.982L12.109 9.27a2.118 2.118 0 00-.58 1.082L11 13l2.648-.53c.41-.082.786-.283 1.082-.579l5.727-5.727a1.853 1.853 0 10-2.621-2.621z"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                  <path
                    d="M19 15v3a2 2 0 01-2 2H6a2 2 0 01-2-2V7a2 2 0 012-2h3"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
              </button>
              <button class="ghost-icon" :aria-label="t('components.mcp.list.delete')" @click="requestDelete(server)">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path
                    d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                  />
                </svg>
              </button>
            </div>
          </article>
        </div>
      </section>
    </div>

    <BaseModal
      :open="modalState.open"
      :title="modalState.editingName ? t('components.mcp.form.editTitle') : t('components.mcp.form.createTitle')"
      @close="closeModal"
    >
      <!-- Tab 切换 -->
      <div class="modal-tabs">
        <button
          type="button"
          class="modal-tab"
          :class="{ active: modalMode === 'form' }"
          @click="switchModalMode('form')"
        >
          {{ t('components.mcp.jsonImport.tabForm') }}
        </button>
        <button
          type="button"
          class="modal-tab"
          :class="{ active: modalMode === 'json' }"
          @click="switchModalMode('json')"
        >
          {{ t('components.mcp.jsonImport.tabJson') }}
        </button>
      </div>

      <div class="modal-scroll">
      <!-- 表单模式 -->
      <form v-if="modalMode === 'form'" class="vendor-form" @submit.prevent="submitModal">
        <div class="form-row">
          <label class="form-field">
            <span>{{ t('components.mcp.form.name') }}</span>
            <BaseInput v-model="modalState.form.name" type="text" :disabled="saveBusy" />
          </label>
          <label class="form-field">
            <span>{{ t('components.mcp.form.website') }}</span>
            <BaseInput v-model="modalState.form.website" type="text" :disabled="saveBusy" placeholder="https://example.com" />
          </label>
        </div>
        <label class="form-field">
          <span>{{ t('components.mcp.form.type') }}</span>
          <select v-model="modalState.form.type" :disabled="saveBusy" class="base-input">
            <option value="stdio">{{ t('components.mcp.types.stdio') }}</option>
            <option value="http">{{ t('components.mcp.types.http') }}</option>
          </select>
        </label>
        <label v-if="modalState.form.type === 'stdio'" class="form-field">
          <span>{{ t('components.mcp.form.command') }}</span>
          <BaseInput v-model="modalState.form.command" type="text" :disabled="saveBusy" />
        </label>
        <label v-if="modalState.form.type === 'stdio'" class="form-field">
          <span>{{ t('components.mcp.form.args') }}</span>
          <BaseTextarea
            v-model="modalState.form.argsText"
            :placeholder="t('components.mcp.form.argsHint')"
            :disabled="saveBusy"
            rows="5"
          />
        </label>
        <label v-if="modalState.form.type === 'http'" class="form-field">
          <span>{{ t('components.mcp.form.url') }}</span>
          <BaseInput v-model="modalState.form.url" type="text" :disabled="saveBusy" />
        </label>
        <label class="form-field">
          <span>{{ t('components.mcp.form.tips') }}</span>
          <BaseTextarea
            v-model="modalState.form.tips"
            :placeholder="t('components.mcp.form.tipsHint')"
            :disabled="saveBusy"
            rows="4"
          />
        </label>
        <div class="form-field">
          <span>{{ t('components.mcp.form.env') }}</span>
          <div class="env-table">
            <div v-for="entry in modalState.form.envEntries" :key="entry.id" class="env-row">
              <BaseInput v-model="entry.key" :placeholder="t('components.mcp.form.envKey')" :disabled="saveBusy" />
              <BaseInput v-model="entry.value" :placeholder="t('components.mcp.form.envValue')" :disabled="saveBusy" />
              <button
                class="ghost-icon"
                type="button"
                :aria-label="t('components.mcp.form.envRemove')"
                :disabled="modalState.form.envEntries.length === 1 || saveBusy"
                @click="removeEnvEntry(entry.id)"
              >
                ✕
              </button>
            </div>
          </div>
          <BaseButton variant="outline" type="button" class="env-add" :disabled="saveBusy" @click="addEnvEntry()">
            {{ t('components.mcp.form.envAdd') }}
          </BaseButton>
        </div>
        <div class="form-field">
          <span>{{ t('components.mcp.form.platforms.title') }}</span>
          <div class="platform-checkboxes">
            <label v-for="option in platformOptions" :key="option.id" class="platform-checkbox">
              <input
                type="checkbox"
                :checked="modalState.form.enablePlatform.includes(option.id)"
                :disabled="saveBusy"
                @change="onModalPlatformToggle(option.id, $event)"
              />
              <span>{{ option.label }}</span>
            </label>
          </div>
        </div>

        <p v-if="modalError" class="alert-error">{{ modalError }}</p>

        <footer class="form-actions">
          <BaseButton variant="outline" type="button" :disabled="saveBusy" @click="closeModal">
            {{ t('components.mcp.form.actions.cancel') }}
          </BaseButton>
          <BaseButton :disabled="saveBusy" type="submit">
            {{ t('components.mcp.form.actions.save') }}
          </BaseButton>
        </footer>
      </form>

      <!-- JSON 导入模式 -->
      <div v-else-if="modalMode === 'json'" class="json-import-section">
        <!-- JSON 输入区 -->
        <div v-if="!jsonParseResult" class="json-input-area">
          <!-- 说明 + 示例按钮 -->
          <div class="flex items-center justify-between mb-3">
            <span class="text-sm text-[var(--mac-text-secondary)]">
              {{ t('components.mcp.jsonImport.jsonHint') }}
            </span>
            <button
              type="button"
              class="text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              @click="fillExampleJson"
            >
              {{ t('components.mcp.jsonImport.loadExample') }}
            </button>
          </div>

          <!-- 格式说明 -->
          <div class="text-xs text-[var(--mac-text-secondary)] bg-blue-50 dark:bg-blue-900/20 p-3 rounded-lg mb-3 space-y-1">
            <div>✅ Claude Desktop: <code class="px-1 bg-white dark:bg-gray-800 rounded">{"mcpServers": {"name": {...}}}</code></div>
            <div>✅ {{ t('components.mcp.jsonImport.formatSingle') }}: <code class="px-1 bg-white dark:bg-gray-800 rounded">{"command": "...", "args": [...]}</code></div>
            <div>✅ {{ t('components.mcp.jsonImport.formatArray') }}: <code class="px-1 bg-white dark:bg-gray-800 rounded">[{...}, {...}]</code></div>
          </div>

          <label class="form-field">
            <span>{{ t('components.mcp.jsonImport.inputLabel') }}</span>
            <BaseTextarea
              v-model="jsonInput"
              :placeholder="t('components.mcp.jsonImport.inputPlaceholder')"
              :disabled="jsonParsing"
              rows="10"
              class="json-textarea"
            />
          </label>
          <p v-if="jsonError" class="alert-error">{{ jsonError }}</p>
          <p class="json-hint">{{ t('components.mcp.jsonImport.formatHint') }}</p>
          <footer class="form-actions">
            <BaseButton variant="outline" type="button" :disabled="jsonParsing" @click="closeModal">
              {{ t('components.mcp.form.actions.cancel') }}
            </BaseButton>
            <BaseButton :disabled="jsonParsing || !jsonInput.trim()" @click="handleParseJSON">
              {{ jsonParsing ? t('components.mcp.jsonImport.parsing') : t('components.mcp.jsonImport.parse') }}
            </BaseButton>
          </footer>
        </div>

        <!-- 解析结果预览 -->
        <div v-else class="json-preview-area">
          <div class="preview-header">
            <span class="preview-count">{{ t('components.mcp.jsonImport.serverCount', { count: jsonParseResult.servers.length }) }}</span>
            <button type="button" class="ghost-icon sm" @click="resetJsonImport" :title="t('components.mcp.jsonImport.reset')">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M6 6l12 12M6 18L18 6" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" fill="none" />
              </svg>
            </button>
          </div>

          <!-- 冲突警告 -->
          <div v-if="jsonParseResult.conflicts.length > 0" class="conflict-warning">
            <p>{{ t('components.mcp.jsonImport.conflictWarning', { names: jsonParseResult.conflicts.join(', ') }) }}</p>
            <div class="conflict-actions">
              <BaseButton variant="outline" size="sm" @click="handleBatchImport('skip')">
                {{ t('components.mcp.jsonImport.conflictSkip') }}
              </BaseButton>
              <BaseButton variant="outline" size="sm" @click="handleBatchImport('overwrite')">
                {{ t('components.mcp.jsonImport.conflictOverwrite') }}
              </BaseButton>
              <BaseButton variant="outline" size="sm" @click="handleBatchImport('rename')">
                {{ t('components.mcp.jsonImport.conflictRename') }}
              </BaseButton>
            </div>
          </div>

          <!-- 服务器列表预览 -->
          <div class="preview-list">
            <div v-for="server in jsonParseResult.servers" :key="server.name" class="preview-item">
              <div class="preview-item-header">
                <span class="preview-item-name">{{ server.name || t('components.mcp.jsonImport.unnamed') }}</span>
                <span class="preview-item-type">{{ server.type }}</span>
              </div>
              <p class="preview-item-detail">
                {{ server.type === 'http' ? server.url : server.command }}
              </p>
            </div>
          </div>

          <footer class="form-actions">
            <BaseButton variant="outline" type="button" @click="closeModal">
              {{ t('components.mcp.form.actions.cancel') }}
            </BaseButton>
            <BaseButton
              v-if="jsonParseResult.conflicts.length === 0"
              :disabled="saveBusy"
              @click="handleBatchImport('skip')"
            >
              {{ t('components.mcp.jsonImport.importAll') }}
            </BaseButton>
          </footer>
        </div>
      </div>
      </div>
    </BaseModal>

    <BaseModal
      :open="confirmState.open"
      :title="t('components.mcp.form.deleteTitle')"
      variant="confirm"
      @close="closeConfirm"
    >
      <div class="confirm-body">
        <p>
          {{ t('components.mcp.form.deleteMessage', { name: confirmState.target?.name ?? '' }) }}
        </p>
      </div>
      <footer class="form-actions confirm-actions">
        <BaseButton variant="outline" type="button" :disabled="saveBusy" @click="closeConfirm">
          {{ t('components.mcp.form.actions.cancel') }}
        </BaseButton>
        <BaseButton variant="danger" type="button" :disabled="saveBusy" @click="confirmDelete">
          {{ t('components.mcp.form.actions.delete') }}
        </BaseButton>
      </footer>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import BaseInput from '../common/BaseInput.vue'
import BaseTextarea from '../common/BaseTextarea.vue'
import {
  fetchMcpServers,
  saveMcpServers,
  parseMCPJSON,
  importMCPFromJSON,
  type McpPlatform,
  type McpServer,
  type McpServerType,
  type MCPParseResult,
  type ConflictStrategy,
} from '../../services/mcp'
import lobeIcons from '../../icons/lobeIconMap'
import { showToast } from '../../utils/toast'

type EnvEntry = {
  id: number
  key: string
  value: string
}

type McpForm = {
  name: string
  type: McpServerType
  command: string
  url: string
  website: string
  tips: string
  argsText: string
  envEntries: EnvEntry[]
  enablePlatform: McpPlatform[]
}

const { t } = useI18n()
const router = useRouter()

const servers = ref<McpServer[]>([])
const loading = ref(false)
const saveBusy = ref(false)
const errorMessage = ref('')
const modalError = ref('')
const placeholderRegex = /\{([a-zA-Z0-9_]+)\}/g

let envEntryId = 0

const createEnvEntry = (key = '', value = ''): EnvEntry => ({
  id: ++envEntryId,
  key,
  value,
})

const createEmptyForm = (): McpForm => ({
  name: '',
  type: 'stdio',
  command: '',
  url: '',
  website: '',
  tips: '',
  argsText: '',
  envEntries: [createEnvEntry()],
  enablePlatform: [],
})

const modalState = reactive({
  open: false,
  editingName: '',
  form: createEmptyForm(),
})

// JSON 导入模式相关状态
type ModalMode = 'form' | 'json'
const modalMode = ref<ModalMode>('form')
const jsonInput = ref('')
const jsonParsing = ref(false)
const jsonError = ref('')
const jsonParseResult = ref<MCPParseResult | null>(null)

const confirmState = reactive<{ open: boolean; target: McpServer | null }>({
  open: false,
  target: null,
})

const platformOptions = computed(() => [
  { id: 'claude-code' as McpPlatform, label: t('components.mcp.platforms.claude') },
  { id: 'codex' as McpPlatform, label: t('components.mcp.platforms.codex') },
])

const formMissingPlaceholders = computed(() => detectPlaceholders(modalState.form.url, modalState.form.argsText))

const loadServers = async () => {
  loading.value = true
  errorMessage.value = ''
  try {
    const data = await fetchMcpServers()
    servers.value = (data ?? []).map((item) => ({
      ...item,
      args: item.args ?? [],
      env: item.env ?? {},
      enable_platform: item.enable_platform ?? [],
      website: item.website ?? '',
      tips: item.tips ?? '',
      missing_placeholders: item.missing_placeholders ?? [],
    }))
  } catch (error) {
    console.error('failed to load mcp servers', error)
    errorMessage.value = t('components.mcp.list.loadError')
  } finally {
    loading.value = false
  }
}

const persistServers = async () => {
  saveBusy.value = true
  try {
    await saveMcpServers(servers.value)
    await loadServers()
  } catch (error) {
    console.error('failed to save mcp servers', error)
    errorMessage.value = t('components.mcp.list.saveError')
  } finally {
    saveBusy.value = false
  }
}

const iconSvg = (name: string) => {
  if (!name) return lobeIcons['mcp'] ?? ''
  const key = name.toLowerCase()
  return lobeIcons[key] ?? lobeIcons['mcp'] ?? ''
}

const iconStyle = (name: string) => ({
  backgroundColor: 'rgba(255,255,255,0.08)',
  color: 'var(--text-primary)',
})

const serverInitials = (name: string) => {
  if (!name) return 'MC'
  return name
    .split(/\s+/)
    .filter(Boolean)
    .map((word) => word[0])
    .join('')
    .slice(0, 2)
    .toUpperCase()
}

const serverSummary = (server: McpServer) => {
  if (server.type === 'http' && server.url) {
    return `${t('components.mcp.types.httpShort')} · ${server.url}`
  }
  if (server.command) {
    return `${t('components.mcp.types.stdioShort')} · ${server.command}`
  }
  return server.type === 'http' ? t('components.mcp.types.httpShort') : t('components.mcp.types.stdioShort')
}

const typeLabel = (type: McpServerType) =>
  type === 'http' ? t('components.mcp.types.http') : t('components.mcp.types.stdio')

const platformEnabled = (server: McpServer, platform: McpPlatform) =>
  server.enable_platform?.includes(platform) ?? false

const platformActive = (server: McpServer, platform: McpPlatform) =>
  platform === 'claude-code' ? server.enabled_in_claude : server.enabled_in_codex

const hasMissingPlaceholders = (server: McpServer) => (server.missing_placeholders?.length ?? 0) > 0

const showPlaceholderWarning = (variables: string[]) => {
  const list = (variables ?? []).filter(Boolean)
  showToast(t('components.mcp.toast.placeholder', { vars: list.join(', ') || 'variables' }), 'error')
}

const onModalPlatformToggle = (platform: McpPlatform, event: Event) => {
  const targetInput = event.target as HTMLInputElement | null
  if (!targetInput) return

  if (formMissingPlaceholders.value.length > 0) {
    targetInput.checked = modalState.form.enablePlatform.includes(platform)
    showPlaceholderWarning(formMissingPlaceholders.value)
    return
  }

  const next = new Set<McpPlatform>(modalState.form.enablePlatform)
  if (targetInput.checked) {
    next.add(platform)
  } else {
    next.delete(platform)
  }
  modalState.form.enablePlatform = Array.from(next)
}

const onPlatformToggle = async (server: McpServer, platform: McpPlatform, event: Event) => {
  const targetInput = event.target as HTMLInputElement | null
  if (!targetInput) return

  if (hasMissingPlaceholders(server)) {
    targetInput.checked = platformEnabled(server, platform)
    showPlaceholderWarning(server.missing_placeholders ?? [])
    return
  }

  const target = servers.value.find((item) => item.name === server.name)
  if (!target) return

  const next = new Set<McpPlatform>(target.enable_platform ?? [])
  if (targetInput.checked) {
    next.add(platform)
  } else {
    next.delete(platform)
  }
  target.enable_platform = Array.from(next)
  await persistServers()
}

const openCreateModal = () => {
  modalState.open = true
  modalState.editingName = ''
  modalState.form = createEmptyForm()
  modalError.value = ''
}

const openEditModal = (server: McpServer) => {
  modalState.open = true
  modalState.editingName = server.name
  modalError.value = ''
  modalState.form = {
    name: server.name,
    type: server.type,
    command: server.command ?? '',
    url: server.url ?? '',
    website: server.website ?? '',
    tips: server.tips ?? '',
    argsText: (server.args ?? []).join('\n'),
    envEntries: buildEnvEntries(server.env),
    enablePlatform: [...(server.enable_platform ?? [])],
  }
}

const closeModal = () => {
  modalState.open = false
  modalState.editingName = ''
  modalState.form = createEmptyForm()
  modalError.value = ''
  // 重置 JSON 导入状态
  modalMode.value = 'form'
  jsonInput.value = ''
  jsonError.value = ''
  jsonParseResult.value = null
}

// 切换 Modal 模式
const switchModalMode = (mode: ModalMode) => {
  modalMode.value = mode
  jsonError.value = ''
  modalError.value = ''
}

// 填充示例 JSON
const fillExampleJson = () => {
  jsonInput.value = JSON.stringify({
    "command": "npx",
    "args": ["-y", "@anthropic-ai/mcp-server-google-maps"],
    "env": {
      "GOOGLE_MAPS_API_KEY": "{YOUR_API_KEY}"
    }
  }, null, 2)
  jsonError.value = ''
}

// 解析 JSON 输入
const handleParseJSON = async () => {
  const input = jsonInput.value.trim()
  if (!input) {
    jsonError.value = t('components.mcp.jsonImport.emptyInput')
    return
  }

  jsonParsing.value = true
  jsonError.value = ''
  jsonParseResult.value = null

  try {
    const result = await parseMCPJSON(input)
    jsonParseResult.value = result

    // 单服务器且需要命名：填充表单并切换到表单模式
    if (result.servers.length === 1 && result.needName) {
      fillFormFromServer(result.servers[0])
      modalMode.value = 'form'
      showToast(t('components.mcp.jsonImport.fillForm'), 'success')
      return
    }

    // 单服务器且已有名称：直接显示导入预览
    if (result.servers.length === 1 && !result.needName) {
      // 保持在 JSON 模式显示预览
    }

    // 多服务器：显示预览列表
  } catch (error) {
    console.error('Failed to parse MCP JSON:', error)
    jsonError.value = error instanceof Error ? error.message : t('components.mcp.jsonImport.parseError')
  } finally {
    jsonParsing.value = false
  }
}

// 从服务器配置填充表单
const fillFormFromServer = (server: McpServer) => {
  modalState.form = {
    name: server.name || '',
    type: server.type || 'stdio',
    command: server.command || '',
    url: server.url || '',
    website: server.website || '',
    tips: server.tips || '',
    argsText: (server.args || []).join('\n'),
    envEntries: buildEnvEntries(server.env),
    enablePlatform: [...(server.enable_platform || [])],
  }
}

// 批量导入服务器
const handleBatchImport = async (strategy: ConflictStrategy = 'skip') => {
  if (!jsonParseResult.value?.servers.length) return

  saveBusy.value = true
  try {
    const count = await importMCPFromJSON(jsonParseResult.value.servers, strategy)
    showToast(t('components.mcp.jsonImport.importSuccess', { count }), 'success')
    closeModal()
    await loadServers()
  } catch (error) {
    console.error('Failed to import MCP servers:', error)
    showToast(t('components.mcp.jsonImport.importError'), 'error')
  } finally {
    saveBusy.value = false
  }
}

// 重置 JSON 导入状态
const resetJsonImport = () => {
  jsonInput.value = ''
  jsonError.value = ''
  jsonParseResult.value = null
}

const buildEnvEntries = (env: Record<string, string> | undefined) => {
  const entries = Object.entries(env ?? {})
  if (!entries.length) {
    return [createEnvEntry()]
  }
  return entries.map(([key, value]) => createEnvEntry(key, value))
}

const addEnvEntry = () => {
  modalState.form.envEntries.push(createEnvEntry())
}

const removeEnvEntry = (id: number) => {
  if (modalState.form.envEntries.length === 1) return
  const index = modalState.form.envEntries.findIndex((entry) => entry.id === id)
  if (index !== -1) {
    modalState.form.envEntries.splice(index, 1)
  }
}

const closeConfirm = () => {
  confirmState.open = false
  confirmState.target = null
}

const requestDelete = (server: McpServer) => {
  confirmState.target = server
  confirmState.open = true
}

const confirmDelete = async () => {
  if (!confirmState.target) return
  servers.value = servers.value.filter((server) => server.name !== confirmState.target?.name)
  closeConfirm()
  await persistServers()
}

const submitModal = async () => {
  modalError.value = ''
  const form = modalState.form
  const trimmedName = form.name.trim()
  if (!trimmedName) {
    modalError.value = t('components.mcp.form.errors.name')
    return
  }
  if (form.type === 'stdio' && !form.command.trim()) {
    modalError.value = t('components.mcp.form.errors.command')
    return
  }
  if (form.type === 'http' && !form.url.trim()) {
    modalError.value = t('components.mcp.form.errors.url')
    return
  }

  // 平台校验：至少勾选一个平台
  if (form.enablePlatform.length === 0) {
    modalError.value = t('components.mcp.form.errors.noPlatformSelected')
    return
  }

  const existing = servers.value.find((server) => server.name === trimmedName)
  if (!modalState.editingName && existing) {
    modalError.value = t('components.mcp.form.errors.duplicate')
    return
  }
  if (modalState.editingName && modalState.editingName !== trimmedName && existing) {
    modalError.value = t('components.mcp.form.errors.duplicate')
    return
  }

  const payload: McpServer = {
    name: trimmedName,
    type: form.type,
    command: form.type === 'stdio' ? form.command.trim() : '',
    args: parseArgs(form.argsText),
    env: parseEnv(form.envEntries),
    url: form.type === 'http' ? form.url.trim() : '',
    website: form.website.trim(),
    tips: form.tips.trim(),
    enable_platform: [...form.enablePlatform],
    enabled_in_claude:
      modalState.editingName === trimmedName
        ? existing?.enabled_in_claude ?? false
        : servers.value.find((server) => server.name === modalState.editingName)?.enabled_in_claude ?? false,
    enabled_in_codex:
      modalState.editingName === trimmedName
        ? existing?.enabled_in_codex ?? false
        : servers.value.find((server) => server.name === modalState.editingName)?.enabled_in_codex ?? false,
    missing_placeholders: [],
  }

  if (modalState.editingName) {
    const index = servers.value.findIndex((server) => server.name === modalState.editingName)
    if (index !== -1) {
      servers.value.splice(index, 1, payload)
    } else {
      servers.value.push(payload)
    }
  } else {
    servers.value.push(payload)
  }

  // 检查占位符并提示
  const placeholders = formMissingPlaceholders.value
  if (placeholders.length > 0 && form.enablePlatform.length > 0) {
    // 显示警告（允许保存，但提示未同步）
    showToast(
      t('components.mcp.form.warnings.savedWithPlaceholders', {
        vars: placeholders.join(', ')
      }),
      'warning'
    )
  }

  closeModal()
  await persistServers()
}

const parseArgs = (value: string) =>
  value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)

const parseEnv = (entries: EnvEntry[]) => {
  return entries.reduce<Record<string, string>>((acc, entry) => {
    const key = entry.key.trim()
    if (!key) return acc
    acc[key] = entry.value
    return acc
  }, {})
}

const goHome = () => {
  router.push('/')
}

const goToSettings = () => {
  router.push('/settings')
}

const reload = async () => {
  await loadServers()
}

const detectPlaceholders = (url: string, argsText: string) => {
  const set = new Set<string>()
  collectPlaceholders(url, set)
  argsText
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .forEach((line) => collectPlaceholders(line, set))
  return Array.from(set)
}

const collectPlaceholders = (value: string, set: Set<string>) => {
  if (!value) return
  const matches = value.matchAll(placeholderRegex)
  for (const match of matches) {
    const key = match[1]
    if (key) {
      set.add(key)
    }
  }
}

onMounted(() => {
  void loadServers()
})
</script>

<style scoped>
.chip {
  padding: 2px 8px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.08);
  font-size: 12px;
  text-transform: uppercase;
}

.card-platforms {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  flex: 1;
}

.platform-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 1rem;
}

.platform-info {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.75rem;
  width: 100%;
}

.platform-label {
  font-weight: 600;
}

.platform-controls {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.platform-status {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.6);
}

.platform-status.active {
  color: #4ade80;
}

.card-actions {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  align-items: flex-end;
}

.empty-state {
  text-align: center;
  padding: 2rem;
  border: 1px dashed rgba(255, 255, 255, 0.2);
  border-radius: 16px;
}

.alert-error {
  margin-bottom: 1rem;
  padding: 0.75rem 1rem;
  border-radius: 12px;
  background: rgba(244, 67, 54, 0.15);
  color: #ff9b9b;
}

.vendor-form {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.modal-scroll {
  max-height: 65vh;
  overflow-y: auto;
  padding-right: 0.25rem;
  margin-right: -0.25rem;
}

.form-field {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}

.form-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 1rem;
  width: 100%;
}

.env-table {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.env-row {
  display: grid;
  grid-template-columns: 1fr 1fr auto;
  gap: 0.5rem;
  align-items: center;
}

.env-add {
  align-self: flex-start;
}

.platform-checkboxes {
  display: flex;
  flex-wrap: wrap;
  gap: 0.75rem;
}

.platform-checkbox {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.75rem;
}

.card-leading {
  display: flex;
  gap: 1rem;
}

.card-icon {
  display: inline-flex;
  justify-content: center;
  align-items: center;
  width: 48px;
  height: 48px;
  border-radius: 14px;
  overflow: hidden;
}

.card-text {
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
}

.card-link {
  margin-top: 0.25rem;
}

.card-link a {
  color: var(--link-color, #9acaff);
  text-decoration: none;
}

.card-link a:hover {
  text-decoration: underline;
}

.card-tip {
  margin-top: 0.25rem;
  font-size: 13px;
  color: rgba(255, 255, 255, 0.7);
}

.icon-svg :deep(svg) {
  width: 32px;
  height: 32px;
}

.confirm-body {
  margin-bottom: 1rem;
}

/* Modal Tab 切换 */
.modal-tabs {
  display: flex;
  gap: 0;
  margin-bottom: 1rem;
  border-bottom: 1px solid var(--mac-border);
}

.modal-tab {
  padding: 0.75rem 1.25rem;
  background: none;
  border: none;
  color: var(--mac-text-secondary);
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  position: relative;
  transition: color 0.2s;
}

.modal-tab:hover {
  color: var(--mac-text);
}

.modal-tab.active {
  color: var(--mac-accent);
  font-weight: 600;
}

.modal-tab.active::after {
  content: '';
  position: absolute;
  bottom: -1px;
  left: 0;
  right: 0;
  height: 2px;
  background: var(--mac-accent);
  border-radius: 1px 1px 0 0;
}

/* JSON 导入区域 */
.json-import-section {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.json-input-area {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.json-textarea {
  font-family: monospace;
  font-size: 12px;
  line-height: 1.5;
}

.json-hint {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
  margin: 0;
}

/* 解析结果预览 */
.json-preview-area {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.preview-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.preview-count {
  font-size: 14px;
  font-weight: 600;
  color: var(--accent-color, #4ade80);
}

.ghost-icon.sm {
  width: 28px;
  height: 28px;
}

.ghost-icon.sm svg {
  width: 14px;
  height: 14px;
}

/* 冲突警告 */
.conflict-warning {
  padding: 1rem;
  border-radius: 12px;
  background: rgba(251, 191, 36, 0.15);
  border: 1px solid rgba(251, 191, 36, 0.3);
}

.conflict-warning p {
  margin: 0 0 0.75rem;
  font-size: 13px;
  color: #fbbf24;
}

.conflict-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

/* 服务器列表预览 */
.preview-list {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
  max-height: 300px;
  overflow-y: auto;
}

.preview-item {
  padding: 0.75rem 1rem;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
}

.preview-item-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.25rem;
}

.preview-item-name {
  font-weight: 600;
  font-size: 14px;
}

.preview-item-type {
  font-size: 11px;
  padding: 2px 6px;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.1);
  text-transform: uppercase;
}

.preview-item-detail {
  margin: 0;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.6);
  word-break: break-all;
}
</style>
