<template>
  <Teleport to="body">
    <Transition name="panel-slide">
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

        <main class="panel-content" @click.stop>
          <slot></slot>
        </main>

        <footer v-if="$slots.footer" class="panel-footer" @click.stop>
          <slot name="footer"></slot>
        </footer>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, watch, onBeforeUnmount, nextTick } from 'vue'

const props = withDefaults(
  defineProps<{
    open: boolean
    title: string
    closeLabel?: string
    closeOnBackdrop?: boolean
  }>(),
  {
    closeLabel: 'Close',
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

const handleBackdropClick = (event: MouseEvent) => {
  if (!props.closeOnBackdrop) return
  if (event.target === event.currentTarget) {
    handleClose()
  }
}

const onKeyDown = (e: KeyboardEvent) => {
  if (!props.open) return
  if (e.key !== 'Escape') return
  if (e.isComposing) return

  e.preventDefault()
  e.stopPropagation()
  handleClose()
}

watch(
  () => props.open,
  (isOpen) => {
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
