<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { getUpdateState, restartApp, type UpdateState } from '../../services/update'

const { t } = useI18n()

const visible = ref(false)
const updateState = ref<UpdateState | null>(null)
const isRestarting = ref(false)
let pollInterval: ReturnType<typeof setInterval> | null = null

const version = computed(() => {
  return updateState.value?.latest_known_version || ''
})

async function checkUpdateReady() {
  try {
    const state = await getUpdateState()
    updateState.value = state

    // 当更新准备好时显示通知
    if (state.update_ready && state.latest_known_version) {
      visible.value = true
    }
  } catch (err) {
    console.error('[UpdateNotification] Failed to get update state:', err)
  }
}

async function installNow() {
  if (isRestarting.value) return

  isRestarting.value = true
  try {
    await restartApp()
  } catch (err) {
    console.error('[UpdateNotification] Failed to restart app:', err)
    isRestarting.value = false
  }
}

function dismiss() {
  visible.value = false
}

onMounted(() => {
  // 初始检查
  checkUpdateReady()

  // 每 30 秒轮询一次更新状态
  pollInterval = setInterval(checkUpdateReady, 30000)
})

onUnmounted(() => {
  if (pollInterval) {
    clearInterval(pollInterval)
    pollInterval = null
  }
})
</script>

<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-300 ease-out"
      enter-from-class="translate-y-full opacity-0"
      enter-to-class="translate-y-0 opacity-100"
      leave-active-class="transition duration-200 ease-in"
      leave-from-class="translate-y-0 opacity-100"
      leave-to-class="translate-y-full opacity-0"
    >
      <div
        v-if="visible"
        class="fixed bottom-4 right-4 z-[9999] max-w-sm"
      >
        <div
          class="flex items-center gap-3 rounded-lg bg-white p-4 shadow-lg ring-1 ring-black/5 dark:bg-zinc-800 dark:ring-white/10"
        >
          <!-- 图标 -->
          <div
            class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-100 text-xl dark:bg-emerald-900/30"
          >
            🎉
          </div>

          <!-- 文本内容 -->
          <div class="min-w-0 flex-1">
            <div class="text-sm font-medium text-zinc-900 dark:text-zinc-100">
              {{ t('update.newVersionReady') }}
            </div>
            <div class="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">
              {{ version }}
            </div>
          </div>

          <!-- 操作按钮 -->
          <div class="flex shrink-0 gap-2">
            <button
              type="button"
              :disabled="isRestarting"
              class="rounded-md bg-emerald-600 px-3 py-1.5 text-xs font-medium text-white shadow-sm transition-colors hover:bg-emerald-500 focus:outline-none focus:ring-2 focus:ring-emerald-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-zinc-800"
              @click="installNow"
            >
              <span v-if="isRestarting" class="flex items-center gap-1">
                <svg class="h-3 w-3 animate-spin" viewBox="0 0 24 24" fill="none">
                  <circle
                    class="opacity-25"
                    cx="12"
                    cy="12"
                    r="10"
                    stroke="currentColor"
                    stroke-width="4"
                  />
                  <path
                    class="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  />
                </svg>
                {{ t('update.installing') }}
              </span>
              <span v-else>{{ t('update.installNow') }}</span>
            </button>
            <button
              type="button"
              :disabled="isRestarting"
              class="rounded-md bg-zinc-100 px-3 py-1.5 text-xs font-medium text-zinc-700 transition-colors hover:bg-zinc-200 focus:outline-none focus:ring-2 focus:ring-zinc-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-zinc-700 dark:text-zinc-300 dark:hover:bg-zinc-600 dark:focus:ring-offset-zinc-800"
              @click="dismiss"
            >
              {{ t('update.later') }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
