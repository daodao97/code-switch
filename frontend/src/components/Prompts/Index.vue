<script setup lang="ts">
import { ref, computed, onMounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import MarkdownEditor from '../common/MarkdownEditor.vue'
import {
  GetPrompts,
  UpsertPrompt,
  DeletePrompt,
  EnablePrompt,
  ImportFromFile,
  GetCurrentFileContent
} from '../../../bindings/codeswitch/services/promptservice'
import type { Prompt } from '../../../bindings/codeswitch/services/models'

const { t } = useI18n()

type Platform = 'claude' | 'codex' | 'gemini'

const platforms: { id: Platform; name: string }[] = [
  { id: 'claude', name: 'Claude Code' },
  { id: 'codex', name: 'Codex' },
  { id: 'gemini', name: 'Gemini' }
]

const activePlatform = ref<Platform>('claude')
const prompts = ref<Record<string, Prompt>>({})
const loading = ref(false)
const showModal = ref(false)
const editingPrompt = ref<Prompt | null>(null)
const currentFileContent = ref<string | null>(null)
const nameInputRef = ref<HTMLInputElement | null>(null)

// 表单
const formData = ref({
  id: '',
  name: '',
  content: '',
  description: '',
  enabled: false
})

const promptList = computed(() => Object.values(prompts.value))
const enabledPrompt = computed(() => promptList.value.find(p => p.enabled))
const promptCount = computed(() => promptList.value.length)

async function loadPrompts() {
  loading.value = true
  try {
    prompts.value = await GetPrompts(activePlatform.value)
    currentFileContent.value = await GetCurrentFileContent(activePlatform.value)
  } catch (e) {
    console.error('Failed to load prompts:', e)
  } finally {
    loading.value = false
  }
}

async function handleToggleEnabled(prompt: Prompt) {
  try {
    if (!prompt.enabled) {
      await EnablePrompt(activePlatform.value, prompt.id)
    } else {
      // 禁用：将 enabled 设为 false
      await UpsertPrompt(activePlatform.value, prompt.id, { ...prompt, enabled: false })
    }
    await loadPrompts()
  } catch (e) {
    console.error('Failed to toggle prompt:', e)
  }
}

function openCreateModal() {
  editingPrompt.value = null
  formData.value = {
    id: crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`,
    name: '',
    content: '',
    description: '',
    enabled: false
  }
  showModal.value = true
  // 等待 DOM 更新后聚焦输入框（修复 macOS WebView 键盘输入问题）
  nextTick(() => {
    nameInputRef.value?.focus()
  })
}

async function openEditModal(prompt: Prompt) {
  editingPrompt.value = prompt

  // 如果是已启用的提示词，从文件读取最新内容
  let content = prompt.content
  if (prompt.enabled) {
    try {
      const fileContent = await GetCurrentFileContent(activePlatform.value)
      if (fileContent !== null) {
        content = fileContent
      }
    } catch (e) {
      console.error('Failed to get current file content:', e)
    }
  }

  formData.value = {
    id: prompt.id,
    name: prompt.name,
    content: content,
    description: prompt.description || '',
    enabled: prompt.enabled
  }
  showModal.value = true
  // 等待 DOM 更新后聚焦输入框（修复 macOS WebView 键盘输入问题）
  nextTick(() => {
    nameInputRef.value?.focus()
  })
}

async function savePrompt() {
  try {
    const prompt: Prompt = {
      id: formData.value.id,
      name: formData.value.name,
      content: formData.value.content,
      description: formData.value.description || undefined,
      enabled: formData.value.enabled
    }
    await UpsertPrompt(activePlatform.value, prompt.id, prompt)
    showModal.value = false
    await loadPrompts()
  } catch (e) {
    console.error('Failed to save prompt:', e)
  }
}

async function deletePrompt(id: string) {
  if (!confirm(t('prompts.confirmDelete'))) return
  try {
    await DeletePrompt(activePlatform.value, id)
    await loadPrompts()
  } catch (e) {
    console.error('Failed to delete prompt:', e)
  }
}

async function handleImport() {
  try {
    loading.value = true
    await ImportFromFile(activePlatform.value)
    await loadPrompts()
  } catch (e) {
    console.error('Failed to import:', e)
  } finally {
    loading.value = false
  }
}

watch(activePlatform, () => {
  loadPrompts()
})

onMounted(() => {
  loadPrompts()
})
</script>

<template>
  <div class="prompts-page">
    <!-- Hero Section -->
    <div class="page-hero">
      <p class="hero-eyebrow">{{ t('prompts.hero.eyebrow') }}</p>
      <h1 class="hero-title">{{ t('prompts.hero.title') }}</h1>
      <p class="hero-lead">{{ t('prompts.hero.lead') }}</p>
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

    <!-- Stats Bar -->
    <div class="stats-bar">
      <span class="stat-text">
        {{ t('prompts.stats.total', { count: promptCount }) }}
      </span>
      <span v-if="enabledPrompt" class="stat-enabled">
        {{ t('prompts.stats.enabled') }}: {{ enabledPrompt.name }}
      </span>
    </div>

    <!-- Prompt List -->
    <div class="prompt-list" v-if="!loading">
      <div v-if="promptList.length === 0" class="empty-state">
        <p>{{ t('prompts.empty') }}</p>
      </div>

      <div
        v-for="prompt in promptList"
        :key="prompt.id"
        class="prompt-card"
        :class="{ enabled: prompt.enabled }"
      >
        <div class="prompt-main">
          <button
            class="toggle-switch"
            :class="{ on: prompt.enabled }"
            @click="handleToggleEnabled(prompt)"
          >
            <span class="toggle-slider"></span>
          </button>
          <div class="prompt-info">
            <h3 class="prompt-name">{{ prompt.name }}</h3>
            <p v-if="prompt.description" class="prompt-description">
              {{ prompt.description }}
            </p>
          </div>
        </div>
        <div class="prompt-actions">
          <button class="action-btn" @click="openEditModal(prompt)">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path>
              <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path>
            </svg>
          </button>
          <button class="action-btn danger" @click="deletePrompt(prompt.id)">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="3 6 5 6 21 6"></polyline>
              <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
            </svg>
          </button>
        </div>
      </div>
    </div>

    <div v-else class="loading-state">
      <span>{{ t('prompts.loading') }}</span>
    </div>

    <!-- Action Buttons -->
    <div class="page-actions">
      <button class="primary-btn" @click="openCreateModal">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="12" y1="5" x2="12" y2="19"></line>
          <line x1="5" y1="12" x2="19" y2="12"></line>
        </svg>
        {{ t('prompts.actions.create') }}
      </button>
      <button class="secondary-btn" @click="handleImport" :disabled="loading">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path>
          <polyline points="17 8 12 3 7 8"></polyline>
          <line x1="12" y1="3" x2="12" y2="15"></line>
        </svg>
        {{ t('prompts.actions.import') }}
      </button>
    </div>

    <!-- Edit Modal (不使用 Teleport 以修复 macOS WebView 键盘输入问题) -->
    <div v-if="showModal" class="modal-overlay" @click.self="showModal = false">
      <div class="modal-content" tabindex="-1">
        <h2 class="modal-title">
          {{ editingPrompt ? t('prompts.form.editTitle') : t('prompts.form.createTitle') }}
        </h2>

        <div class="form-group">
          <label>{{ t('prompts.form.name') }}</label>
          <input
            ref="nameInputRef"
            v-model="formData.name"
            type="text"
            class="form-input"
            :placeholder="t('prompts.form.namePlaceholder')"
          />
        </div>

        <div class="form-group">
          <label>{{ t('prompts.form.description') }}</label>
          <input
            v-model="formData.description"
            type="text"
            class="form-input"
            :placeholder="t('prompts.form.descriptionPlaceholder')"
          />
        </div>

        <div class="form-group">
          <label>{{ t('prompts.form.content') }}</label>
          <MarkdownEditor v-model="formData.content" />
        </div>

        <div class="modal-actions">
          <button class="secondary-btn" @click="showModal = false">
            {{ t('prompts.form.cancel') }}
          </button>
          <button class="primary-btn" @click="savePrompt" :disabled="!formData.name">
            {{ t('prompts.form.save') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.prompts-page {
  padding: 24px;
  max-width: 900px;
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

.stats-bar {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 12px 16px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  margin-bottom: 20px;
}

.stat-text {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.stat-enabled {
  font-size: 0.85rem;
  color: #10b981;
  font-weight: 500;
}

.prompt-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-bottom: 24px;
}

.prompt-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 16px;
  transition: all 0.15s ease;
}

.prompt-card:hover {
  border-color: var(--mac-accent);
}

.prompt-card.enabled {
  border-color: #10b981;
  background: rgba(16, 185, 129, 0.05);
}

.prompt-main {
  display: flex;
  align-items: center;
  gap: 16px;
}

.toggle-switch {
  position: relative;
  width: 44px;
  height: 24px;
  border: none;
  border-radius: 999px;
  background: #e2e8f0;
  cursor: pointer;
  transition: background 0.2s ease;
}

html.dark .toggle-switch {
  background: #374151;
}

.toggle-switch.on {
  background: #10b981;
}

.toggle-slider {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 20px;
  height: 20px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.15);
}

.toggle-switch.on .toggle-slider {
  transform: translateX(20px);
}

.prompt-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.prompt-name {
  font-size: 0.95rem;
  font-weight: 600;
  color: var(--mac-text);
}

.prompt-description {
  font-size: 0.8rem;
  color: var(--mac-text-secondary);
}

.prompt-actions {
  display: flex;
  gap: 8px;
}

.action-btn {
  width: 34px;
  height: 34px;
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

.action-btn:hover {
  background: rgba(15, 23, 42, 0.06);
  color: var(--mac-text);
}

html.dark .action-btn:hover {
  background: rgba(255, 255, 255, 0.08);
}

.action-btn.danger:hover {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.1);
}

.action-btn svg {
  width: 16px;
  height: 16px;
}

.empty-state,
.loading-state {
  text-align: center;
  padding: 48px 24px;
  color: var(--mac-text-secondary);
}

.page-actions {
  display: flex;
  gap: 12px;
}

.page-actions .primary-btn,
.page-actions .secondary-btn {
  display: inline-flex;
  flex-direction: row;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  border: none;
  border-radius: 999px;
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  white-space: nowrap;
}

.page-actions .primary-btn {
  background: var(--mac-accent);
  color: #fff;
}

.page-actions .primary-btn:hover:not(:disabled) {
  opacity: 0.9;
}

.page-actions .primary-btn:disabled,
.page-actions .secondary-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.page-actions .secondary-btn {
  background: var(--mac-surface);
  color: var(--mac-text);
  border: 1px solid var(--mac-border);
}

.page-actions .secondary-btn:hover:not(:disabled) {
  border-color: var(--mac-accent);
}

.page-actions .primary-btn svg,
.page-actions .secondary-btn svg {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}

/* Modal */
.modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
}

.modal-content {
  background: var(--mac-surface);
  border-radius: 20px;
  padding: 24px;
  width: 90%;
  max-width: 700px;
  max-height: 90vh;
  overflow-y: auto;
  box-shadow: 0 20px 40px rgba(0, 0, 0, 0.2);
}

.modal-title {
  font-size: 1.25rem;
  font-weight: 700;
  color: var(--mac-text);
  margin-bottom: 24px;
}

.form-group {
  margin-bottom: 20px;
}

.form-group label {
  display: block;
  font-size: 0.85rem;
  font-weight: 500;
  color: var(--mac-text);
  margin-bottom: 8px;
}

.form-input {
  width: 100%;
  padding: 12px 16px;
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  font-size: 0.9rem;
  background: var(--mac-bg);
  color: var(--mac-text);
  transition: all 0.15s ease;
}

.form-input:focus {
  outline: none;
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 3px rgba(10, 132, 255, 0.15);
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
}
</style>
