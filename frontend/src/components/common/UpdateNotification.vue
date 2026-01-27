<template>
  <Transition name="slide-up">
    <div v-if="showNotification" class="update-notification">
      <div class="update-content">
        <!-- 图标 -->
        <div class="update-icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
            <polyline points="7 10 12 15 17 10" />
            <line x1="12" y1="15" x2="12" y2="3" />
          </svg>
        </div>

        <!-- 信息 -->
        <div class="update-info">
          <div class="update-title">
            {{ $t('update.newVersion', { version: state.latest_version }) }}
          </div>
          <div v-if="isDownloading" class="update-progress">
            <div class="progress-bar">
              <div class="progress-fill" :style="{ width: `${progressPercent}%` }"></div>
            </div>
            <span class="progress-text">{{ downloadedMB }} / {{ totalMB }} MB</span>
          </div>
          <div v-else-if="hasError" class="update-error">
            {{ $t('update.error') }}: {{ state.error }}
          </div>
        </div>

        <!-- 操作按钮 -->
        <div class="update-actions">
          <!-- 检查中 -->
          <template v-if="isChecking">
            <span class="checking-text">{{ $t('update.checking') }}</span>
          </template>

          <!-- 有更新可用 -->
          <template v-else-if="state.state === 'available'">
            <button class="btn-update" @click="download">
              {{ $t('update.downloadNow') }}
            </button>
            <button class="btn-dismiss" @click="dismiss">
              {{ $t('update.later') }}
            </button>
          </template>

          <!-- 下载中 -->
          <template v-else-if="isDownloading">
            <button class="btn-cancel" @click="cancel">
              {{ $t('update.cancel') }}
            </button>
          </template>

          <!-- 准备就绪 -->
          <template v-else-if="isReady">
            <button class="btn-update" @click="restart">
              {{ $t('update.restartNow') }}
            </button>
            <button class="btn-dismiss" @click="dismissNotification">
              {{ $t('update.later') }}
            </button>
          </template>

          <!-- 错误 -->
          <template v-else-if="hasError">
            <button v-if="state.error_op === 'download'" class="btn-update" @click="download">
              {{ $t('update.retry') }}
            </button>
            <button class="btn-dismiss" @click="dismissNotification">
              {{ $t('update.close') }}
            </button>
          </template>
        </div>

        <!-- 关闭按钮 -->
        <button class="btn-close" @click="dismissNotification" :title="$t('update.close')">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <line x1="18" y1="6" x2="6" y2="18" />
            <line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useUpdateStore } from '../../composables/useUpdateStore'

const {
  state,
  hasUpdate,
  isChecking,
  isDownloading,
  isReady,
  hasError,
  isDismissed,
  progressPercent,
  downloadedMB,
  totalMB,
  download,
  cancel,
  restart,
  dismiss,
} = useUpdateStore()

// 本地隐藏状态（用户点击关闭后暂时隐藏）
const localHidden = ref(false)

// 显示通知的条件
const showNotification = computed(() => {
  // 用户隐藏了通知
  if (localHidden.value) return false

  // 用户忽略了此版本
  if (isDismissed.value) return false

  // 有更新可用、正在下载、准备就绪、或有错误
  return hasUpdate.value || hasError.value
})

// 当状态变化时重置本地隐藏
watch(() => state.state, () => {
  localHidden.value = false
})

function dismissNotification() {
  localHidden.value = true
}
</script>

<style scoped>
.update-notification {
  position: fixed;
  bottom: 20px;
  right: 20px;
  z-index: 1000;
  max-width: 420px;
  background: var(--mac-card-bg, #fff);
  border: 1px solid var(--mac-border, #e5e5e5);
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12);
  overflow: hidden;
}

:global(.dark) .update-notification {
  background: #1e1e1e;
  border-color: #3a3a3a;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
}

.update-content {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  position: relative;
}

.update-icon {
  flex-shrink: 0;
  width: 36px;
  height: 36px;
  background: linear-gradient(135deg, #007aff, #5856d6);
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.update-icon svg {
  width: 20px;
  height: 20px;
  color: #fff;
}

.update-info {
  flex: 1;
  min-width: 0;
}

.update-title {
  font-weight: 600;
  font-size: 14px;
  color: var(--mac-text, #1d1d1f);
  margin-bottom: 4px;
}

:global(.dark) .update-title {
  color: #f5f5f5;
}

.update-progress {
  display: flex;
  align-items: center;
  gap: 8px;
}

.progress-bar {
  flex: 1;
  height: 4px;
  background: var(--mac-border, #e5e5e5);
  border-radius: 2px;
  overflow: hidden;
}

:global(.dark) .progress-bar {
  background: #3a3a3a;
}

.progress-fill {
  height: 100%;
  background: linear-gradient(90deg, #007aff, #5856d6);
  border-radius: 2px;
  transition: width 0.2s ease;
}

.progress-text {
  font-size: 12px;
  color: var(--mac-secondary, #86868b);
  white-space: nowrap;
}

.update-error {
  font-size: 12px;
  color: #ff3b30;
}

.update-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}

.checking-text {
  font-size: 12px;
  color: var(--mac-secondary, #86868b);
}

.btn-update {
  padding: 6px 12px;
  background: linear-gradient(135deg, #007aff, #5856d6);
  color: #fff;
  border: none;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  transition: opacity 0.2s;
}

.btn-update:hover {
  opacity: 0.9;
}

.btn-dismiss,
.btn-cancel {
  padding: 6px 12px;
  background: transparent;
  color: var(--mac-secondary, #86868b);
  border: 1px solid var(--mac-border, #e5e5e5);
  border-radius: 6px;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.2s, border-color 0.2s;
}

:global(.dark) .btn-dismiss,
:global(.dark) .btn-cancel {
  border-color: #3a3a3a;
}

.btn-dismiss:hover,
.btn-cancel:hover {
  background: var(--mac-hover, #f5f5f5);
  border-color: var(--mac-border-hover, #d1d1d6);
}

:global(.dark) .btn-dismiss:hover,
:global(.dark) .btn-cancel:hover {
  background: #2a2a2a;
}

.btn-close {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 24px;
  height: 24px;
  padding: 0;
  background: transparent;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  opacity: 0.5;
  transition: opacity 0.2s, background 0.2s;
}

.btn-close:hover {
  opacity: 1;
  background: var(--mac-hover, #f5f5f5);
}

:global(.dark) .btn-close:hover {
  background: #2a2a2a;
}

.btn-close svg {
  width: 14px;
  height: 14px;
  color: var(--mac-secondary, #86868b);
}

/* 动画 */
.slide-up-enter-active,
.slide-up-leave-active {
  transition: all 0.3s ease;
}

.slide-up-enter-from,
.slide-up-leave-to {
  transform: translateY(20px);
  opacity: 0;
}
</style>
