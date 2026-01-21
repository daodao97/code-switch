<template>
  <div class="main-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ t('components.gemini.hero.eyebrow') }}</p>
      <button class="ghost-icon" :aria-label="t('components.gemini.controls.back')" @click="goHome">
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
      <button class="ghost-icon" :aria-label="t('components.gemini.controls.settings')" @click="goToSettings">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path
            d="M12 15a3 3 0 100-6 3 3 0 000 6z"
            stroke="currentColor"
            stroke-width="1.5"
            fill="none"
          />
          <path
            d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 01-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-4 0v-.09a1.65 1.65 0 00-1-1.51 1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 010-4h.09a1.65 1.65 0 001.51-1 1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 012.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 014 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 010 4h-.09a1.65 1.65 0 00-1.51 1z"
            stroke="currentColor"
            stroke-width="1.5"
            fill="none"
          />
        </svg>
      </button>
    </div>

    <div class="contrib-page">
      <section class="contrib-hero">
        <h1>{{ t('components.gemini.hero.title') }}</h1>
        <p class="lead">{{ t('components.gemini.hero.lead') }}</p>
      </section>

      <!-- 当前状态 -->
      <section v-if="status" class="status-section">
        <div class="status-card" :class="{ enabled: status?.enabled }">
          <div class="status-icon">
            <span v-html="geminiIcon" aria-hidden="true"></span>
          </div>
          <div class="status-info">
            <p class="status-title">{{ status?.enabled ? t('components.gemini.status.enabled') : t('components.gemini.status.disabled') }}</p>
            <p v-if="status?.currentProvider" class="status-provider">{{ status.currentProvider }}</p>
            <p class="status-auth">{{ authTypeLabel(status?.authType ?? 'gemini-api-key') }}</p>
          </div>
        </div>
      </section>

      <!-- 预设供应商 -->
      <section class="automation-section">
        <div class="section-header">
          <h2 class="section-title">{{ t('components.gemini.presets.title') }}</h2>
          <div class="section-controls">
            <button
              class="ghost-icon"
              :aria-label="t('components.gemini.controls.refresh')"
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
                />
                <path
                  d="M20.5 4v4h-4"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                />
              </svg>
            </button>
          </div>
        </div>

        <div class="preset-grid">
          <article
            v-for="preset in presets"
            :key="preset.name"
            class="preset-card"
            :class="{ official: preset.category === 'official' }"
            @click="openPresetModal(preset)"
          >
            <div class="preset-icon">
              <span v-html="getPresetIcon(preset)" aria-hidden="true"></span>
            </div>
            <div class="preset-info">
              <p class="preset-name">{{ preset.name }}</p>
              <p class="preset-desc">{{ preset.description }}</p>
            </div>
            <span class="preset-category">{{ categoryLabel(preset.category) }}</span>
          </article>
        </div>
      </section>

      <!-- 已配置的供应商 -->
      <section class="automation-section">
        <div class="section-header">
          <h2 class="section-title">{{ t('components.gemini.providers.title') }}</h2>
          <div class="section-controls">
            <button class="ghost-icon" :aria-label="t('components.gemini.controls.create')" @click="openCreateModal">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path
                  d="M12 5v14M5 12h14"
                  stroke="currentColor"
                  stroke-width="1.5"
                  stroke-linecap="round"
                  fill="none"
                />
              </svg>
            </button>
          </div>
        </div>

        <div v-if="loading" class="empty-state">{{ t('components.gemini.list.loading') }}</div>

        <div v-else-if="!providers.length" class="empty-state">
          <p>{{ t('components.gemini.list.empty') }}</p>
        </div>

        <div v-else class="automation-list">
          <article
            v-for="provider in providers"
            :key="provider.id"
            class="automation-card"
            :class="{ active: provider.enabled }"
          >
            <div class="card-leading">
              <div class="card-icon" :style="{ backgroundColor: '#4285F4', color: '#fff' }">
                <span v-html="geminiIcon" aria-hidden="true"></span>
              </div>
              <div class="card-text">
                <div class="card-title-row">
                  <p class="card-title">{{ provider.name }}</p>
                  <span v-if="provider.enabled" class="chip active">{{ t('components.gemini.status.active') }}</span>
                </div>
                <p class="card-metrics">
                  <span v-if="provider.baseUrl">{{ provider.baseUrl }}</span>
                  <span v-if="provider.model"> · {{ provider.model }}</span>
                </p>
              </div>
            </div>
            <div class="card-actions">
              <BaseButton
                v-if="!provider.enabled"
                variant="outline"
                size="sm"
                :disabled="switching"
                @click="switchToProvider(provider.id)"
              >
                {{ t('components.gemini.actions.switch') }}
              </BaseButton>
              <button class="ghost-icon" :aria-label="t('components.gemini.list.edit')" @click="openEditModal(provider)">
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
              <button class="ghost-icon" :aria-label="t('components.gemini.list.delete')" @click="requestDelete(provider)">
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path
                    d="M9 3h6m-7 4h8m-6 0v11m4-11v11M5 7h14l-.867 12.138A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.862L5 7z"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="1.5"
                  />
                </svg>
              </button>
            </div>
          </article>
        </div>
      </section>
    </div>

    <!-- 添加/编辑供应商弹窗 -->
    <BaseModal
      :open="modalState.open"
      :title="modalState.editing ? t('components.gemini.form.editTitle') : t('components.gemini.form.createTitle')"
      @close="closeModal"
    >
      <form class="vendor-form" @submit.prevent="submitModal">
        <label class="form-field">
          <span>{{ t('components.gemini.form.name') }}</span>
          <BaseInput v-model="modalState.form.name" type="text" :disabled="saving" />
        </label>

        <label class="form-field">
          <span>{{ t('components.gemini.form.baseUrl') }}</span>
          <BaseInput v-model="modalState.form.baseUrl" type="text" :disabled="saving" placeholder="https://api.example.com" />
        </label>

        <label class="form-field">
          <span>{{ t('components.gemini.form.apiKey') }}</span>
          <BaseInput
            v-model="modalState.form.apiKey"
            type="password"
            :disabled="saving"
            placeholder="sk-xxx"
          />
        </label>

        <label class="form-field">
          <span>{{ t('components.gemini.form.model') }}</span>
          <BaseInput v-model="modalState.form.model" type="text" :disabled="saving" placeholder="gemini-2.5-pro-preview" />
        </label>

        <footer class="form-actions">
          <BaseButton variant="outline" type="button" @click="closeModal">
            {{ t('components.gemini.form.cancel') }}
          </BaseButton>
          <BaseButton type="submit" :disabled="saving">
            {{ t('components.gemini.form.save') }}
          </BaseButton>
        </footer>
      </form>
    </BaseModal>

    <!-- 删除确认弹窗 -->
    <BaseModal
      :open="confirmState.open"
      :title="t('components.gemini.form.deleteTitle')"
      variant="confirm"
      @close="closeConfirm"
    >
      <div class="confirm-body">
        <p>{{ t('components.gemini.form.deleteMessage', { name: confirmState.provider?.name ?? '' }) }}</p>
      </div>
      <footer class="form-actions confirm-actions">
        <BaseButton variant="outline" type="button" @click="closeConfirm">
          {{ t('components.gemini.form.cancel') }}
        </BaseButton>
        <BaseButton variant="danger" type="button" @click="confirmDelete">
          {{ t('components.gemini.form.delete') }}
        </BaseButton>
      </footer>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import BaseButton from '../common/BaseButton.vue'
import BaseModal from '../common/BaseModal.vue'
import BaseInput from '../common/BaseInput.vue'
import lobeIcons from '../../icons/lobeIconMap'
import {
  GetPresets,
  GetProviders,
  GetStatus,
  AddProvider,
  UpdateProvider,
  DeleteProvider,
  SwitchProvider,
  CreateProviderFromPreset,
} from '../../../bindings/codeswitch/services/geminiservice'

const { t } = useI18n()
const router = useRouter()

const geminiIcon = lobeIcons['gemini'] ?? ''

type BindingGeminiStatus = Awaited<ReturnType<typeof GetStatus>>
type BindingGeminiProvider = Awaited<ReturnType<typeof GetProviders>> extends (infer P)[] ? P : any
type BindingGeminiPreset = Awaited<ReturnType<typeof GetPresets>> extends (infer P)[] ? P : any
type GeminiAuth = BindingGeminiStatus extends { authType: infer A } ? A : string

const loading = ref(false)
const saving = ref(false)
const switching = ref(false)

const presets = ref<BindingGeminiPreset[]>([])
const providers = ref<BindingGeminiProvider[]>([])
const status = ref<BindingGeminiStatus | null>(null)

const modalState = reactive({
  open: false,
  editing: false,
  editingId: '',
  originalProvider: null as BindingGeminiProvider | null,
  presetMode: false,
  presetName: '',
  form: {
    name: '',
    baseUrl: '',
    apiKey: '',
    model: '',
  },
})

const confirmState = reactive({
  open: false,
  provider: null as BindingGeminiProvider | null,
})

const goHome = () => router.push('/')
const goToSettings = () => router.push('/settings')

const reload = async () => {
  loading.value = true
  try {
    const [presetsData, providersData, statusData] = await Promise.all([
      GetPresets(),
      GetProviders(),
      GetStatus(),
    ])
    presets.value = presetsData ?? []
    providers.value = providersData ?? []
    status.value = statusData
  } catch (err) {
    console.error('Failed to load Gemini data:', err)
  } finally {
    loading.value = false
  }
}

const authTypeLabel = (authType: GeminiAuth) => {
  switch (authType) {
    case 'oauth-personal':
      return t('components.gemini.auth.oauth')
    case 'gemini-api-key':
    case 'packycode':
    case 'generic':
      return t('components.gemini.auth.apiKey')
    default:
      return ''
  }
}

const categoryLabel = (category: string) => {
  switch (category) {
    case 'official':
      return t('components.gemini.category.official')
    case 'third_party':
      return t('components.gemini.category.thirdParty')
    case 'custom':
      return t('components.gemini.category.custom')
    default:
      return category
  }
}

const getPresetIcon = (preset: BindingGeminiPreset) => {
  if (preset.category === 'official') {
    return lobeIcons['google'] ?? geminiIcon
  }
  return geminiIcon
}

const openPresetModal = (preset: BindingGeminiPreset) => {
  modalState.open = true
  modalState.editing = false
  modalState.presetMode = true
  modalState.presetName = preset.name
  modalState.form.name = preset.name
  modalState.form.baseUrl = preset.baseUrl ?? ''
  modalState.form.apiKey = ''
  modalState.form.model = preset.model ?? 'gemini-2.5-pro-preview'
}

const openCreateModal = () => {
  modalState.open = true
  modalState.editing = false
  modalState.presetMode = false
  modalState.form.name = ''
  modalState.form.baseUrl = ''
  modalState.form.apiKey = ''
  modalState.form.model = 'gemini-2.5-pro-preview'
}

const openEditModal = (provider: BindingGeminiProvider) => {
  modalState.open = true
  modalState.editing = true
  modalState.editingId = provider.id
  modalState.originalProvider = provider
  modalState.presetMode = false
  modalState.form.name = provider.name
  modalState.form.baseUrl = provider.baseUrl ?? ''
  modalState.form.apiKey = provider.apiKey ?? ''
  modalState.form.model = provider.model ?? ''
}

const closeModal = () => {
  modalState.open = false
  modalState.editing = false
  modalState.editingId = ''
  modalState.originalProvider = null
}

const submitModal = async () => {
  saving.value = true
  try {
    if (modalState.presetMode) {
      // 从预设创建
      const newProvider = await CreateProviderFromPreset(modalState.presetName, modalState.form.apiKey)
      if (!newProvider) {
        throw new Error('创建供应商失败')
      }
    } else if (modalState.editing) {
      // 更新：基于原始对象合并，保留 partnerPromotionKey/category 等字段
      const original = modalState.originalProvider
        ?? providers.value.find(p => p.id === modalState.editingId)
      if (!original) {
        throw new Error('未找到要更新的供应商')
      }

      await UpdateProvider({
        ...original,
        id: modalState.editingId,
        name: modalState.form.name,
        baseUrl: modalState.form.baseUrl,
        apiKey: modalState.form.apiKey,
        model: modalState.form.model,
        enabled: original.enabled, // 保持启用状态不变
        envConfig: {
          ...(original.envConfig ?? {}),
          GOOGLE_GEMINI_BASE_URL: modalState.form.baseUrl,
          GEMINI_API_KEY: modalState.form.apiKey,
          GEMINI_MODEL: modalState.form.model,
        },
      })
    } else {
      // 新建
      await AddProvider({
        id: '',
        name: modalState.form.name,
        baseUrl: modalState.form.baseUrl,
        apiKey: modalState.form.apiKey,
        model: modalState.form.model,
        enabled: false,
        envConfig: {
          GOOGLE_GEMINI_BASE_URL: modalState.form.baseUrl,
          GEMINI_API_KEY: modalState.form.apiKey,
          GEMINI_MODEL: modalState.form.model,
        },
      })
    }
    closeModal()
    await reload()
  } catch (err) {
    console.error('Failed to save provider:', err)
  } finally {
    saving.value = false
  }
}

const switchToProvider = async (id: string) => {
  switching.value = true
  try {
    await SwitchProvider(id)
    await reload()
  } catch (err) {
    console.error('Failed to switch provider:', err)
  } finally {
    switching.value = false
  }
}

const requestDelete = (provider: BindingGeminiProvider) => {
  confirmState.provider = provider
  confirmState.open = true
}

const closeConfirm = () => {
  confirmState.open = false
  confirmState.provider = null
}

const confirmDelete = async () => {
  if (!confirmState.provider) return
  try {
    await DeleteProvider(confirmState.provider.id)
    closeConfirm()
    await reload()
  } catch (err) {
    console.error('Failed to delete provider:', err)
  }
}

onMounted(() => {
  reload()
})
</script>

<style scoped>
.status-section {
  margin-bottom: 24px;
}

.status-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 12px;
}

.status-card.enabled {
  border-color: #10b981;
  background: rgba(16, 185, 129, 0.05);
}

.status-icon {
  width: 48px;
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #4285F4;
  border-radius: 12px;
  color: #fff;
}

.status-icon :deep(svg) {
  width: 28px;
  height: 28px;
}

.status-info {
  flex: 1;
}

.status-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--mac-text);
  margin-bottom: 4px;
}

.status-provider {
  font-size: 14px;
  color: var(--mac-text-secondary);
}

.status-auth {
  font-size: 12px;
  color: var(--mac-text-tertiary);
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--mac-text);
  margin: 0;
}

.preset-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 12px;
}

.preset-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px;
  background: var(--mac-surface);
  border: 1px solid var(--mac-border);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.preset-card:hover {
  border-color: var(--mac-accent);
  background: var(--mac-surface-strong);
}

.preset-card.official {
  border-color: #4285F4;
}

.preset-icon {
  width: 40px;
  height: 40px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #4285F4;
  border-radius: 8px;
  color: #fff;
  flex-shrink: 0;
}

.preset-icon :deep(svg) {
  width: 24px;
  height: 24px;
}

.preset-info {
  flex: 1;
  min-width: 0;
}

.preset-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--mac-text);
  margin-bottom: 2px;
}

.preset-desc {
  font-size: 12px;
  color: var(--mac-text-secondary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.preset-category {
  font-size: 10px;
  padding: 2px 8px;
  background: var(--mac-surface-strong);
  border-radius: 4px;
  color: var(--mac-text-secondary);
  flex-shrink: 0;
}

.automation-card.active {
  border-color: #10b981;
  background: rgba(16, 185, 129, 0.05);
}

.chip.active {
  background: #10b981;
  color: #fff;
}
</style>
