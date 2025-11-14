<template>
  <div class="model-whitelist-editor">
    <div class="editor-header">
      <label class="editor-label">
        <span>{{ $t('components.provider.modelWhitelist.label') }}</span>
        <button
          type="button"
          class="help-icon"
          :data-tooltip="$t('components.provider.modelWhitelist.tooltip')"
        >
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <path
              d="M8 1a7 7 0 100 14A7 7 0 008 1zm0 13A6 6 0 118 2a6 6 0 010 12zm0-9.5a.75.75 0 01.75.75v4a.75.75 0 01-1.5 0v-4A.75.75 0 018 4.5zm0 7.5a1 1 0 100-2 1 1 0 000 2z"
              fill="currentColor"
            />
          </svg>
        </button>
      </label>
    </div>

    <!-- 已添加的模型列表 -->
    <div v-if="modelList.length > 0" class="model-tags">
      <div
        v-for="(model, index) in modelList"
        :key="index"
        class="model-tag"
      >
        <span class="model-name" :class="{ wildcard: isWildcard(model) }">{{ model }}</span>
        <button
          type="button"
          class="tag-remove"
          :aria-label="$t('components.provider.modelWhitelist.remove')"
          @click="removeModel(index)"
        >
          <svg viewBox="0 0 12 12" width="10" height="10" aria-hidden="true">
            <path
              d="M3 3l6 6M9 3l-6 6"
              stroke="currentColor"
              stroke-width="1.5"
              stroke-linecap="round"
            />
          </svg>
        </button>
      </div>
    </div>

    <!-- 添加新模型输入框 -->
    <div class="model-input-row">
      <BaseInput
        v-model="newModel"
        type="text"
        :placeholder="$t('components.provider.modelWhitelist.placeholder')"
        @keydown.enter.prevent="addModel"
      />
      <BaseButton
        type="button"
        variant="outline"
        @click="addModel"
      >
        {{ $t('components.provider.modelWhitelist.add') }}
      </BaseButton>
    </div>

    <!-- 通配符示例和说明 -->
    <div class="help-text">
      <p class="help-example">
        <strong>{{ $t('components.provider.modelWhitelist.examples.title') }}</strong>
      </p>
      <ul class="help-list">
        <li>
          <code>claude-sonnet-4</code> - {{ $t('components.provider.modelWhitelist.examples.exact') }}
        </li>
        <li>
          <code>claude-*</code> - {{ $t('components.provider.modelWhitelist.examples.prefix') }}
        </li>
        <li>
          <code>anthropic/claude-*</code> - {{ $t('components.provider.modelWhitelist.examples.vendor') }}
        </li>
      </ul>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import BaseInput from './BaseInput.vue'
import BaseButton from './BaseButton.vue'

interface Props {
  modelValue?: Record<string, boolean>
}

interface Emits {
  (e: 'update:modelValue', value: Record<string, boolean>): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

// 将 Record<string, boolean> 转换为数组便于展示
const modelList = computed(() => {
  if (!props.modelValue) return []
  return Object.keys(props.modelValue).filter((key) => props.modelValue![key])
})

const newModel = ref('')

const isWildcard = (model: string) => model.includes('*')

const addModel = () => {
  const trimmed = newModel.value.trim()
  if (!trimmed) return

  // 检查是否已存在
  if (props.modelValue && props.modelValue[trimmed]) {
    newModel.value = ''
    return
  }

  // 添加到模型列表
  const updated = { ...props.modelValue }
  updated[trimmed] = true
  emit('update:modelValue', updated)
  newModel.value = ''
}

const removeModel = (index: number) => {
  const modelName = modelList.value[index]
  if (!modelName) return

  const updated = { ...props.modelValue }
  delete updated[modelName]
  emit('update:modelValue', updated)
}

// 初始化空对象
watch(
  () => props.modelValue,
  (value) => {
    if (value === undefined) {
      emit('update:modelValue', {})
    }
  },
  { immediate: true }
)
</script>

<style scoped>
.model-whitelist-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.editor-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.editor-label {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  font-size: 0.875rem;
  color: var(--foreground);
}

.help-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px;
  border: none;
  background: none;
  color: var(--foreground-muted);
  cursor: help;
  border-radius: 4px;
  transition: all 0.2s;
}

.help-icon:hover {
  color: var(--foreground);
  background-color: var(--background-hover);
}

.model-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px;
  background-color: var(--background-secondary);
  border-radius: 8px;
  min-height: 44px;
}

.model-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 8px 4px 10px;
  background-color: var(--background);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 0.8125rem;
  line-height: 1.4;
  transition: all 0.2s;
}

.model-tag:hover {
  background-color: var(--background-hover);
}

.model-name {
  color: var(--foreground);
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
}

.model-name.wildcard {
  color: var(--accent-primary);
  font-weight: 500;
}

.tag-remove {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px;
  border: none;
  background: none;
  color: var(--foreground-muted);
  cursor: pointer;
  border-radius: 3px;
  transition: all 0.2s;
}

.tag-remove:hover {
  color: var(--error);
  background-color: var(--error-bg);
}

.model-input-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.model-input-row :deep(input) {
  flex: 1;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
}

.help-text {
  padding: 12px;
  background-color: var(--background-secondary);
  border-radius: 8px;
  font-size: 0.8125rem;
  color: var(--foreground-muted);
}

.help-example {
  margin-bottom: 8px;
  color: var(--foreground);
}

.help-list {
  margin: 0;
  padding-left: 20px;
  list-style: disc;
}

.help-list li {
  margin-bottom: 4px;
  line-height: 1.5;
}

.help-list code {
  padding: 2px 6px;
  background-color: var(--background);
  border: 1px solid var(--border);
  border-radius: 4px;
  font-family: 'SF Mono', 'Menlo', 'Monaco', 'Courier New', monospace;
  font-size: 0.75rem;
  color: var(--accent-primary);
}
</style>
