<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="show" class="modal-backdrop" @click.self="handleCancel">
        <div class="modal-container">
          <div class="modal-header">
            <h3>{{ t('deeplink.title') }}</h3>
            <button class="modal-close" @click="handleCancel">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M18 6L6 18M6 6l12 12"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                />
              </svg>
            </button>
          </div>

          <div class="modal-body">
            <!-- 错误状态 -->
            <div v-if="error" class="error-message">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2" />
                <path d="M12 8v4M12 16h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
              <div>
                <p class="error-title">{{ t('deeplink.error.title') }}</p>
                <p class="error-detail">{{ error }}</p>
              </div>
            </div>

            <!-- 成功状态 -->
            <div v-else-if="imported" class="success-message">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <circle cx="12" cy="12" r="10" fill="none" stroke="currentColor" stroke-width="2" />
                <path d="M9 12l2 2 4-4" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
              <p>{{ t('deeplink.success') }}</p>
            </div>

            <!-- 正在导入 -->
            <div v-else-if="importing" class="loading-message">
              <div class="spinner"></div>
              <p>{{ t('deeplink.importing') }}</p>
            </div>

            <!-- 预览信息 -->
            <div v-else-if="request" class="preview-container">
              <p class="preview-hint">{{ t('deeplink.preview') }}</p>

              <div class="preview-field">
                <span class="field-label">{{ t('deeplink.field.name') }}</span>
                <span class="field-value">{{ request.name }}</span>
              </div>

              <div class="preview-field">
                <span class="field-label">{{ t('deeplink.field.app') }}</span>
                <span class="field-value app-badge" :class="`app-${request.app}`">
                  {{ request.app }}
                </span>
              </div>

              <div v-if="request.homepage" class="preview-field">
                <span class="field-label">{{ t('deeplink.field.homepage') }}</span>
                <a :href="request.homepage" target="_blank" class="field-link">
                  {{ request.homepage }}
                </a>
              </div>

              <div v-if="request.endpoint" class="preview-field">
                <span class="field-label">{{ t('deeplink.field.endpoint') }}</span>
                <span class="field-value mono">{{ request.endpoint }}</span>
              </div>

              <div v-if="request.apiKey" class="preview-field">
                <span class="field-label">{{ t('deeplink.field.apiKey') }}</span>
                <span class="field-value mono masked">{{ maskApiKey(request.apiKey) }}</span>
              </div>

              <div v-if="request.model" class="preview-field">
                <span class="field-label">{{ t('deeplink.field.model') }}</span>
                <span class="field-value">{{ request.model }}</span>
              </div>

              <div v-if="request.notes" class="preview-field">
                <span class="field-label">{{ t('deeplink.field.notes') }}</span>
                <span class="field-value">{{ request.notes }}</span>
              </div>
            </div>
          </div>

          <div class="modal-footer">
            <button v-if="error || imported" class="btn-secondary" @click="handleClose">
              {{ t('common.close') }}
            </button>
            <template v-else>
              <button class="btn-secondary" @click="handleCancel" :disabled="importing">
                {{ t('common.cancel') }}
              </button>
              <button class="btn-primary" @click="handleImport" :disabled="importing">
                {{ t('deeplink.import') }}
              </button>
            </template>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DeepLinkImportRequest } from '../../bindings/codeswitch/services/deeplinkservice'
import * as DeepLinkService from '../../bindings/codeswitch/services/deeplinkservice'

const { t } = useI18n()

const props = defineProps<{
  url: string
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  imported: [providerId: string]
}>()

const request = ref<DeepLinkImportRequest | null>(null)
const error = ref<string>('')
const importing = ref(false)
const imported = ref(false)

// 解析 URL
watch(() => props.url, async (newUrl) => {
  if (newUrl && props.show) {
    try {
      error.value = ''
      request.value = await DeepLinkService.ParseDeepLinkURL(newUrl)
    } catch (err: any) {
      error.value = err.message || String(err)
      request.value = null
    }
  }
}, { immediate: true })

// 导入供应商
const handleImport = async () => {
  if (!request.value || importing.value) return

  try {
    importing.value = true
    error.value = ''

    const providerId = await DeepLinkService.ImportProviderFromDeepLink(request.value)

    imported.value = true

    // 2秒后自动关闭
    setTimeout(() => {
      emit('imported', providerId)
      handleClose()
    }, 2000)
  } catch (err: any) {
    error.value = err.message || String(err)
  } finally {
    importing.value = false
  }
}

const handleCancel = () => {
  if (importing.value) return
  handleClose()
}

const handleClose = () => {
  request.value = null
  error.value = ''
  importing.value = false
  imported.value = false
  emit('close')
}

// 隐藏 API Key 中间部分
const maskApiKey = (key: string): string => {
  if (key.length <= 8) return '*'.repeat(key.length)
  return key.slice(0, 4) + '*'.repeat(key.length - 8) + key.slice(-4)
}
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 1rem;
}

.modal-container {
  background: var(--bg-primary);
  border-radius: 12px;
  box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04);
  width: 100%;
  max-width: 560px;
  max-height: 90vh;
  display: flex;
  flex-direction: column;
}

.modal-header {
  padding: 1.5rem;
  border-bottom: 1px solid var(--border-color);
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.modal-header h3 {
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.modal-close {
  background: none;
  border: none;
  padding: 0.5rem;
  cursor: pointer;
  color: var(--text-secondary);
  border-radius: 6px;
  transition: all 0.15s;
}

.modal-close:hover {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.modal-close svg {
  width: 20px;
  height: 20px;
  display: block;
}

.modal-body {
  padding: 1.5rem;
  overflow-y: auto;
  flex: 1;
}

.error-message, .success-message, .loading-message {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  padding: 1rem;
  border-radius: 8px;
}

.error-message {
  background: rgba(239, 68, 68, 0.1);
  color: var(--color-error);
}

.error-message svg {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  margin-top: 0.125rem;
}

.error-title {
  font-weight: 600;
  margin: 0 0 0.25rem;
}

.error-detail {
  font-size: 0.875rem;
  margin: 0;
  opacity: 0.9;
}

.success-message {
  background: rgba(34, 197, 94, 0.1);
  color: var(--color-success);
  align-items: center;
}

.success-message svg {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
}

.success-message p {
  margin: 0;
  font-weight: 500;
}

.loading-message {
  align-items: center;
  justify-content: center;
  padding: 2rem;
}

.spinner {
  width: 24px;
  height: 24px;
  border: 3px solid var(--bg-secondary);
  border-top-color: var(--color-primary);
  border-radius: 50%;
  animation: spin 0.6s linear infinite;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.loading-message p {
  margin: 0;
  color: var(--text-secondary);
}

.preview-container {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.preview-hint {
  color: var(--text-secondary);
  font-size: 0.875rem;
  margin: 0 0 0.5rem;
}

.preview-field {
  display: grid;
  grid-template-columns: 100px 1fr;
  gap: 1rem;
  align-items: center;
  padding: 0.75rem;
  background: var(--bg-secondary);
  border-radius: 6px;
}

.field-label {
  font-size: 0.875rem;
  font-weight: 500;
  color: var(--text-secondary);
}

.field-value {
  font-size: 0.875rem;
  color: var(--text-primary);
  word-break: break-all;
}

.field-value.mono {
  font-family: 'SF Mono', 'Consolas', 'Monaco', monospace;
  font-size: 0.8125rem;
}

.field-value.masked {
  letter-spacing: 0.05em;
}

.field-link {
  font-size: 0.875rem;
  color: var(--color-primary);
  text-decoration: none;
  word-break: break-all;
}

.field-link:hover {
  text-decoration: underline;
}

.app-badge {
  display: inline-block;
  padding: 0.25rem 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.app-badge.app-claude {
  background: rgba(167, 139, 250, 0.2);
  color: #a78bfa;
}

.app-badge.app-codex {
  background: rgba(96, 165, 250, 0.2);
  color: #60a5fa;
}

.app-badge.app-gemini {
  background: rgba(251, 146, 60, 0.2);
  color: #fb923c;
}

.modal-footer {
  padding: 1.5rem;
  border-top: 1px solid var(--border-color);
  display: flex;
  gap: 0.75rem;
  justify-content: flex-end;
}

.btn-primary, .btn-secondary {
  padding: 0.625rem 1.25rem;
  border-radius: 6px;
  font-size: 0.875rem;
  font-weight: 500;
  border: none;
  cursor: pointer;
  transition: all 0.15s;
}

.btn-primary {
  background: var(--color-primary);
  color: white;
}

.btn-primary:hover:not(:disabled) {
  opacity: 0.9;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-secondary {
  background: var(--bg-secondary);
  color: var(--text-primary);
}

.btn-secondary:hover:not(:disabled) {
  background: var(--bg-tertiary);
}

.btn-secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Modal transition */
.modal-enter-active, .modal-leave-active {
  transition: opacity 0.2s ease;
}

.modal-enter-from, .modal-leave-to {
  opacity: 0;
}

.modal-enter-active .modal-container,
.modal-leave-active .modal-container {
  transition: transform 0.2s ease;
}

.modal-enter-from .modal-container,
.modal-leave-to .modal-container {
  transform: scale(0.95);
}
</style>
