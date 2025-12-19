<template>
  <textarea
    ref="textareaEl"
    v-bind="$attrs"
    class="base-textarea"
    :value="modelValue"
    autocorrect="off"
    autocapitalize="none"
    spellcheck="false"
    @input="onInput"
  />
</template>

<script setup lang="ts">
import { ref, useAttrs } from 'vue'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
  defineProps<{
    modelValue?: string
  }>(),
  {
    modelValue: '',
  },
)

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

useAttrs()

const textareaEl = ref<HTMLTextAreaElement | null>(null)

const focus = () => textareaEl.value?.focus()

defineExpose({ focus })

const onInput = (event: Event) => {
  const target = event.target as HTMLTextAreaElement
  emit('update:modelValue', target.value)
}
</script>
