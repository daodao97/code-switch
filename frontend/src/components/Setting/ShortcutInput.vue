<script setup lang="ts">
import { ref, computed, watch, onMounted, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ modelValue: string }>()
const emit = defineEmits(['update:modelValue'])

const { t } = useI18n()

const modifierKeys = ['Shift', 'Control', 'Alt', 'Meta'] as const
const modifierSymbols: Record<string, string> = {
  Shift: '⇧',
  Control: '⌃',
  Alt: '⌥',
  Meta: '⌘',
}

const normalizeModifier = (key: string): string => {
  switch (key.toUpperCase()) {
    case 'CMD':
    case 'COMMAND':
      return 'Meta'
    case 'CTRL':
      return 'Control'
    case 'OPTION':
      return 'Alt'
    default:
      return key
  }
}

const pressedModifiers = ref<Set<string>>(new Set())
const finalModifiers = ref<Set<string>>(new Set())
const mainKey = ref<string | null>(null)

const isRecording = ref(false)
const isFinalized = ref(false)

const activeModifiers = computed(() => {
  const activeSet = isFinalized.value ? finalModifiers.value : pressedModifiers.value
  return modifierKeys.map(key => ({
    key,
    symbol: modifierSymbols[key],
    active: activeSet.has(key),
  }))
})

const mainKeyDisplay = computed(() => (mainKey.value ? mainKey.value.toUpperCase() : ''))

const shortcutString = computed(() => {
  if (!isFinalized.value || !mainKey.value || finalModifiers.value.size === 0) {
    return ''
  }
  return `${[...finalModifiers.value].join('+')}+${mainKey.value.toUpperCase()}`
})

watch(shortcutString, (val) => {
  emit('update:modelValue', val)
})

const startRecording = () => {
  isRecording.value = true
  isFinalized.value = false
  mainKey.value = null
  pressedModifiers.value.clear()
  finalModifiers.value.clear()
}

const stopRecording = () => {
  isRecording.value = false
}

const finalize = () => {
  if (pressedModifiers.value.size === 0 || !mainKey.value) {
    startRecording()
    return
  }
  isFinalized.value = true
  finalModifiers.value = new Set(pressedModifiers.value)
}

const clearAll = () => {
  isRecording.value = false
  isFinalized.value = false
  pressedModifiers.value.clear()
  finalModifiers.value.clear()
  mainKey.value = null
  emit('update:modelValue', '')
}

const onKeyDown = (e: KeyboardEvent) => {
  if (!isRecording.value || isFinalized.value) return

  if (modifierKeys.includes(e.key as any)) {
    pressedModifiers.value.add(e.key)
  } else if (!['Tab', 'Escape'].includes(e.key)) {
    mainKey.value = e.key.length === 1 ? e.key.toUpperCase() : e.key
    finalize()
  }
  e.preventDefault()
}

const onKeyUp = (e: KeyboardEvent) => {
  if (!isRecording.value || isFinalized.value) return
  if (modifierKeys.includes(e.key as any)) {
    pressedModifiers.value.delete(e.key)
  }
}

onMounted(() => {
  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('keyup', onKeyUp)
})

watch(() => props.modelValue, (val) => {
  if (!val) {
    clearAll()
    return
  }

  const parts = val.split('+').map(normalizeModifier)
  const mods = parts.filter(part => modifierKeys.includes(part as any))
  const key = parts.find(part => !modifierKeys.includes(part as any))

  if (mods.length && key) {
    finalModifiers.value = new Set(mods)
    mainKey.value = key
    isFinalized.value = true
  } else {
    clearAll()
  }
}, { immediate: true })

const placeholderText = computed(() => t('components.shortcut.placeholder'))
</script>

<template>
  <div
    class="mac-shortcut-input"
    tabindex="0"
    @focus="startRecording"
    @click="startRecording"
    @blur="stopRecording"
  >
    <div class="mac-shortcut-display">
      <span
        v-for="modifier in activeModifiers"
        :key="modifier.key"
        :class="['modifier-symbol', { active: modifier.active }]"
      >
        {{ modifier.symbol }}
      </span>
      <span v-if="mainKeyDisplay" class="main-key">{{ mainKeyDisplay }}</span>
      <span v-else class="mac-shortcut-placeholder">{{ placeholderText }}</span>
    </div>
    <button
      v-if="isFinalized && mainKeyDisplay"
      type="button"
      class="mac-shortcut-clear"
      @click.stop="clearAll"
      aria-label="Clear shortcut"
    >
      ✕
    </button>
  </div>
</template>

<style scoped>
.mac-shortcut-input {
  min-width: 190px;
  display: inline-flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 14px;
  border-radius: 12px;
  border: 1px solid var(--mac-border);
  background: var(--mac-surface-strong);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.12);
  cursor: text;
  transition: border-color 0.2s ease, box-shadow 0.2s ease;
}

.mac-shortcut-input:focus-within {
  border-color: var(--mac-accent);
  box-shadow: 0 0 0 3px rgba(10, 132, 255, 0.25); /* fallback for old WebKit */
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--mac-accent) 25%, transparent);
}

.mac-shortcut-display {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  letter-spacing: 0.02em;
}

.modifier-symbol {
  color: var(--mac-text-secondary);
  transition: color 0.2s ease;
}

.modifier-symbol.active {
  color: var(--mac-accent);
}

.main-key {
  color: var(--mac-accent);
}

.mac-shortcut-placeholder {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.mac-shortcut-clear {
  border: none;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 0.85rem;
  cursor: pointer;
  padding: 4px;
  border-radius: 50%;
  transition: background 0.2s ease, color 0.2s ease;
}

.mac-shortcut-clear:hover {
  background: rgba(110, 110, 115, 0.15); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-text-secondary) 15%, transparent);
  color: var(--mac-text);
}
</style>
