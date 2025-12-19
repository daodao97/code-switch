<template>
  <Transition name="modal-fade">
    <div v-if="open" class="modal-backdrop" role="presentation">
      <!-- 遮罩层：仅负责视觉，不接收点击（避免 WebView 命中测试/层合成导致误触关闭） -->
      <div class="modal-overlay-noevent" aria-hidden="true"></div>

      <!-- 点击空白处关闭：只有点到 wrapper 自身时才触发 -->
      <div class="modal-wrapper" @click="onWrapperClick">
        <Transition name="modal-slide" appear>
          <div
            ref="panelRef"
            :class="['modal', variantClass]"
            role="dialog"
            aria-modal="true"
            :aria-labelledby="titleId"
            tabindex="-1"
          >
            <header class="modal-header">
              <h2 :id="titleId" class="modal-title">{{ title }}</h2>
              <button
                ref="closeButtonRef"
                class="ghost-icon"
                type="button"
                aria-label="Close"
                @click="emitClose"
              >
                ✕
              </button>
            </header>
            <div class="modal-body modal-scrollable">
              <slot />
            </div>
          </div>
        </Transition>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

type Variant = 'default' | 'confirm'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    variant?: Variant
    closeOnBackdrop?: boolean
  }>(),
  { variant: 'default', closeOnBackdrop: true },
)

const emit = defineEmits<{ (e: 'close'): void }>()

const variantClass = computed(() => (props.variant === 'confirm' ? 'confirm-modal' : ''))
const titleId = `modal-title-${Math.random().toString(36).slice(2, 9)}`

const panelRef = ref<HTMLElement | null>(null)
const closeButtonRef = ref<HTMLButtonElement | null>(null)
let lastActiveElement: Element | null = null

const emitClose = () => emit('close')

// 统一阻断冒泡；只有点到 wrapper 空白处才关闭（等价于 @click.self）
const onWrapperClick = (event: MouseEvent) => {
  event.stopPropagation()
  if (!props.closeOnBackdrop) return
  if (event.target === event.currentTarget) {
    emitClose()
  }
}

const getFocusableElements = (): HTMLElement[] => {
  if (!panelRef.value) return []
  const selector = [
    'a[href]',
    'button:not([disabled])',
    'input:not([disabled]):not([type="hidden"])',
    'select:not([disabled])',
    'textarea:not([disabled])',
    '[tabindex]:not([tabindex="-1"])',
  ].join(',')
  return Array.from(panelRef.value.querySelectorAll<HTMLElement>(selector)).filter((el) => {
    const style = getComputedStyle(el)
    return style.display !== 'none' && style.visibility !== 'hidden'
  })
}

const onKeyDown = (e: KeyboardEvent) => {
  if (!props.open) return

  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emitClose()
    return
  }

  if (e.key !== 'Tab') return

  const focusables = getFocusableElements()
  if (focusables.length === 0) {
    e.preventDefault()
    panelRef.value?.focus()
    return
  }

  const active = document.activeElement as HTMLElement | null
  const first = focusables[0]
  const last = focusables[focusables.length - 1]
  const inside = active && panelRef.value?.contains(active)

  if (e.shiftKey) {
    if (!inside || active === first) {
      e.preventDefault()
      last.focus()
    }
  } else {
    if (!inside || active === last) {
      e.preventDefault()
      first.focus()
    }
  }
}

const lockScroll = () => {
  document.body.style.overflow = 'hidden'
  const mainContent = document.querySelector('.main-content') as HTMLElement | null
  if (mainContent) mainContent.style.overflow = 'hidden'
}

const unlockScroll = () => {
  document.body.style.overflow = ''
  const mainContent = document.querySelector('.main-content') as HTMLElement | null
  if (mainContent) mainContent.style.overflow = ''
}

watch(
  () => props.open,
  (open) => {
    if (open) {
      lastActiveElement = document.activeElement
      window.addEventListener('keydown', onKeyDown, true)
      lockScroll()
      nextTick(() => closeButtonRef.value?.focus())
    } else {
      window.removeEventListener('keydown', onKeyDown, true)
      unlockScroll()
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
  window.removeEventListener('keydown', onKeyDown, true)
  unlockScroll()
})
</script>

<style scoped>
.modal-fade-enter-active,
.modal-fade-leave-active {
  transition: opacity 0.2s ease;
}
.modal-fade-enter-from,
.modal-fade-leave-to {
  opacity: 0;
}

.modal-slide-enter-active,
.modal-slide-leave-active {
  transition: all 0.2s ease;
}
.modal-slide-enter-from,
.modal-slide-leave-to {
  opacity: 0;
  transform: translateY(16px);
}
</style>
