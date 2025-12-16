<template>
  <!-- DEBUG: 临时禁用 Teleport 测试是否传送目标问题 -->
    <div
      v-if="open"
        v-bind="$attrs"
        ref="panelRef"
        class="panel-container"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="titleId"
        tabindex="-1"
        @click="handleBackdropClick"
        @keydown="onKeyDown"
      >
        <!-- Header: @click.stop 防止点击 header 触发 backdrop 逻辑 -->
        <header class="panel-header" @click.stop>
          <button
            ref="closeButtonRef"
            class="back-button"
            type="button"
            :aria-label="closeLabel"
            @click="handleClose"
          >
            <svg xmlns="http://www.w3.org/2000/svg" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <polyline points="15 18 9 12 15 6"></polyline>
            </svg>
          </button>
          <h2 :id="titleId" class="panel-title">{{ title }}</h2>
          <div class="header-spacer"></div>
        </header>

        <!--
          Main Content: @click.stop 是关键修复
          创建"事件隔离堡垒"，确保内容区域（Tab按钮、表单等）的点击
          不会冒泡到 panel-container 触发意外关闭
        -->
        <main class="panel-content" @click.stop>
          <slot></slot>
        </main>

        <!-- Footer: 同样需要 @click.stop -->
        <footer v-if="$slots.footer" class="panel-footer" @click.stop>
          <slot name="footer"></slot>
        </footer>
      </div>
</template>

<script setup lang="ts">
import { ref, watch, onBeforeUnmount, nextTick } from 'vue'
import { showToast } from '../../utils/toast'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    closeLabel?: string
    closeOnBackdrop?: boolean
  }>(),
  {
    closeLabel: 'Close',
    // 全屏面板默认不支持点击背景关闭，防止误操作导致数据丢失
    closeOnBackdrop: false,
  },
)

const emit = defineEmits<{
  (e: 'close'): void
}>()

const titleId = `panel-title-${Math.random().toString(36).slice(2, 9)}`
const panelRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
let lastActiveElement: Element | null = null

const handleClose = () => {
  emit('close')
}

// 简化的背景点击处理：仅当 closeOnBackdrop 启用且点击的是容器本身时关闭
const handleBackdropClick = (event: MouseEvent) => {
  if (!props.closeOnBackdrop) return
  // 严格检查：只有点击容器本身才关闭（子元素的 @click.stop 已阻止冒泡）
  if (event.target === event.currentTarget) {
    handleClose()
  }
}

// 简化的 Escape 键处理
const onKeyDown = (e: KeyboardEvent) => {
  if (!props.open) return
  if (e.key !== 'Escape') return
  // 忽略 IME 输入法组合状态
  if (e.isComposing) return

  e.preventDefault()
  e.stopPropagation()
  handleClose()
}

// 焦点管理和 body 滚动锁定
watch(
  () => props.open,
  (isOpen) => {
    showToast(`[DEBUG] FullScreenPanel watch: open=${isOpen}, title=${props.title}`, 'success')
    if (isOpen) {
      lastActiveElement = document.activeElement
      document.body.style.overflow = 'hidden'
      nextTick(() => closeButtonRef.value?.focus())
    } else {
      document.body.style.overflow = ''
      if (lastActiveElement instanceof HTMLElement) {
        try {
          lastActiveElement.focus()
        } catch {
          /* ignore */
        }
      }
      lastActiveElement = null
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  document.body.style.overflow = ''
})
</script>

<style scoped>
.panel-container {
  position: fixed;
  inset: 0;
  z-index: 2000;
  background-color: var(--mac-surface);
  color: var(--mac-text);
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.panel-header {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  padding: 12px 20px;
  border-bottom: 1px solid var(--mac-border);
  flex-shrink: 0;
  text-align: center;
}

.panel-title {
  font-size: 1.1rem;
  font-weight: 600;
  color: var(--mac-text);
  grid-column: 2;
  line-height: 1.5;
  margin: 0;
  padding: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.back-button {
  grid-column: 1;
  background: rgba(128, 128, 128, 0.1);
  border: none;
  padding: 8px;
  margin: 0;
  cursor: pointer;
  color: var(--mac-text-secondary);
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 8px;
  transition: background-color 0.2s ease;
  width: 36px;
  height: 36px;
}

.back-button:hover {
  background-color: rgba(128, 128, 128, 0.2);
}

.back-button:focus-visible {
  outline: 2px solid var(--mac-accent);
  outline-offset: 2px;
}

.header-spacer {
  grid-column: 3;
  width: 36px;
}

.panel-content {
  flex-grow: 1;
  overflow-y: auto;
  padding: 24px;
  -webkit-overflow-scrolling: touch;
}

.panel-footer {
  flex-shrink: 0;
  padding: 16px 24px;
  border-top: 1px solid var(--mac-border);
  background-color: var(--mac-surface);
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

/* Transition 动画 */
.panel-slide-enter-active,
.panel-slide-leave-active {
  transition: transform 0.3s cubic-bezier(0.16, 1, 0.3, 1);
}

.panel-slide-enter-from,
.panel-slide-leave-to {
  transform: translateY(100%);
}

.panel-slide-enter-to,
.panel-slide-leave-from {
  transform: translateY(0);
}

@media (prefers-reduced-motion: reduce) {
  .panel-slide-enter-active,
  .panel-slide-leave-active {
    transition: none;
  }
}
</style>
