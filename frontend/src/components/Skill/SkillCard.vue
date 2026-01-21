<template>
  <article class="skill-card-installed">
    <div class="skill-card-header">
      <div class="skill-card-toggle">
        <button
          type="button"
          :class="['toggle-switch', { enabled: skill.enabled }]"
          :disabled="toggling"
          :title="t('components.skill.actions.toggle')"
          @click="$emit('toggle', skill, !skill.enabled)"
        >
          <span class="toggle-slider" :class="{ toggling: toggling }"></span>
        </button>
      </div>

      <div class="skill-card-info">
        <div class="skill-card-meta">
          <p class="skill-card-eyebrow">{{ skill.directory }}</p>
          <h3 class="skill-card-title">{{ skill.name }}</h3>
        </div>
        <p class="skill-card-desc">
          {{ skill.description || t('components.skill.list.noDescription') }}
        </p>
        <div class="skill-card-badges">
          <span v-if="skill.license_file" class="skill-badge license">
            {{ t('components.skill.license.complete', { file: skill.license_file }) }}
          </span>
          <span v-if="skill.install_location === 'project'" class="skill-badge project">
            {{ t('components.skill.groups.project') }}
          </span>
        </div>
      </div>

      <div class="skill-card-actions">
        <button
          type="button"
          class="ghost-icon sm"
          :title="expanded ? t('components.skill.actions.hideContent') : t('components.skill.actions.viewContent')"
          :data-tooltip="expanded ? t('components.skill.actions.hideContent') : t('components.skill.actions.viewContent')"
          @click="$emit('expand', skill)"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ rotated: expanded }">
            <path d="M6 9l6 6 6-6" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"
              stroke-linejoin="round" />
          </svg>
        </button>
        <button
          v-if="skill.readme_url"
          type="button"
          class="ghost-icon sm"
          :title="t('components.skill.actions.view')"
          :data-tooltip="t('components.skill.actions.view')"
          @click="$emit('view', skill.readme_url)"
        >
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path d="M12 5h7v7M19 5l-9 9" fill="none" stroke="currentColor" stroke-width="1.6"
              stroke-linecap="round" stroke-linejoin="round" />
            <path d="M11 6H7a2 2 0 00-2 2v9a2 2 0 002 2h9a2 2 0 002-2v-4" fill="none" stroke="currentColor"
              stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
        </button>
        <button
          type="button"
          class="ghost-icon sm danger"
          :title="t('components.skill.actions.uninstall')"
          :data-tooltip="t('components.skill.actions.uninstall')"
          :disabled="uninstalling"
          @click="$emit('uninstall', skill)"
        >
          <svg v-if="!uninstalling" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M5 7h14M10 11v6M14 11v6M9 7V5h6v2" fill="none" stroke="currentColor" stroke-width="1.6"
              stroke-linecap="round" stroke-linejoin="round" />
            <path d="M6.5 7l-.5 12a2 2 0 002 2h8a2 2 0 002-2L17.5 7" fill="none" stroke="currentColor"
              stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span v-else class="skill-action-spinner" aria-hidden="true"></span>
        </button>
      </div>
    </div>

    <transition name="expand">
      <div v-if="expanded" class="skill-card-content">
        <div v-if="loadingContent" class="skill-content-loading">
          {{ t('components.skill.actions.loading') }}
        </div>
        <pre v-else class="skill-content-pre">{{ content }}</pre>
      </div>
    </transition>
  </article>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getSkillContent, type SkillSummary } from '../../services/skill'

const props = defineProps<{
  skill: SkillSummary
  expanded: boolean
  toggling: boolean
  uninstalling: boolean
}>()

defineEmits<{
  toggle: [skill: SkillSummary, enabled: boolean]
  expand: [skill: SkillSummary]
  uninstall: [skill: SkillSummary]
  view: [url: string]
}>()

const { t } = useI18n()

const content = ref('')
const loadingContent = ref(false)

// Load content when expanded
watch(() => props.expanded, async (isExpanded) => {
  if (isExpanded && !content.value) {
    loadingContent.value = true
    try {
      content.value = await getSkillContent(
        props.skill.directory,
        props.skill.platform || 'claude',
        props.skill.install_location || 'user'
      )
    } catch (error) {
      console.error('failed to load skill content', error)
      content.value = t('components.skill.actions.loadFailed')
    } finally {
      loadingContent.value = false
    }
  }
})
</script>

<style scoped>
.skill-card-installed {
  background: var(--mac-surface-strong); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 90%, transparent);
  border: 1px solid var(--mac-border);
  border-radius: 16px;
  overflow: hidden;
}

.skill-card-header {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: 16px 20px;
}

.skill-card-toggle {
  flex-shrink: 0;
  padding-top: 4px;
}

.toggle-switch {
  position: relative;
  width: 44px;
  height: 24px;
  border-radius: 12px;
  border: none;
  background: var(--mac-border);
  cursor: pointer;
  transition: background 0.2s ease;
}

.toggle-switch.enabled {
  background: #22c55e;
}

.toggle-switch:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.toggle-slider {
  position: absolute;
  top: 2px;
  left: 2px;
  width: 20px;
  height: 20px;
  background: white;
  border-radius: 50%;
  transition: transform 0.2s ease;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
}

.toggle-switch.enabled .toggle-slider {
  transform: translateX(20px);
}

.toggle-slider.toggling {
  opacity: 0.7;
}

.skill-card-info {
  flex: 1;
  min-width: 0;
}

.skill-card-meta {
  margin-bottom: 8px;
}

.skill-card-eyebrow {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.15em;
  color: var(--mac-text-secondary);
  margin: 0 0 4px;
}

.skill-card-title {
  font-size: 1rem;
  font-weight: 600;
  margin: 0;
}

.skill-card-desc {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
  line-height: 1.4;
  margin: 0 0 8px;
}

.skill-card-badges {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.skill-badge {
  font-size: 0.75rem;
  padding: 2px 8px;
  border-radius: 4px;
  background: var(--mac-surface);
  color: var(--mac-text-secondary);
}

.skill-badge.license {
  background: rgba(245, 158, 11, 0.15); /* fallback for old WebKit */
  background: color-mix(in srgb, #f59e0b 15%, transparent);
  color: #f59e0b;
}

.skill-badge.project {
  background: rgba(59, 130, 246, 0.15); /* fallback for old WebKit */
  background: color-mix(in srgb, #3b82f6 15%, transparent);
  color: #3b82f6;
}

.skill-card-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.skill-card-actions .ghost-icon {
  width: 32px;
  height: 32px;
}

.skill-card-actions .ghost-icon svg {
  width: 18px;
  height: 18px;
  transition: transform 0.2s ease;
}

.skill-card-actions .ghost-icon svg.rotated {
  transform: rotate(180deg);
}

.skill-card-actions .ghost-icon.danger {
  color: #ef4444;
}

.skill-card-actions .ghost-icon:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.skill-action-spinner {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid currentColor;
  border-top-color: transparent;
  animation: skill-spin 0.8s linear infinite;
  display: inline-block;
}

/* Content Panel */
.skill-card-content {
  border-top: 1px solid var(--mac-border);
  background: var(--mac-surface-strong); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 50%, transparent);
  max-height: 400px;
  overflow: auto;
}

.skill-content-loading {
  padding: 16px 20px;
  color: var(--mac-text-secondary);
  font-size: 0.9rem;
}

.skill-content-pre {
  margin: 0;
  padding: 16px 20px;
  font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
  font-size: 0.8rem;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--mac-text);
}

/* Expand Transition */
.expand-enter-active,
.expand-leave-active {
  transition: all 0.2s ease;
  max-height: 400px;
}

.expand-enter-from,
.expand-leave-to {
  max-height: 0;
  opacity: 0;
}

/* Dark Mode */
html.dark .skill-card-installed {
  background: var(--mac-surface); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 70%, transparent);
}

html.dark .skill-card-content {
  background: var(--mac-surface); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 30%, transparent);
}

@keyframes skill-spin {
  from {
    transform: rotate(0deg);
  }
  to {
    transform: rotate(360deg);
  }
}
</style>
