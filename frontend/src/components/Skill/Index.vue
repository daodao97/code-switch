<template>
  <div class="main-shell">
    <div class="global-actions">
      <p class="global-eyebrow">{{ t('components.skill.hero.eyebrow') }}</p>
      <button class="ghost-icon" :title="t('components.skill.actions.back')"
        :data-tooltip="t('components.skill.actions.back')" @click="goHome">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M15 18l-6-6 6-6" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
            stroke-linejoin="round" />
        </svg>
      </button>
      <button class="ghost-icon" :title="t('components.skill.actions.refresh')"
        :data-tooltip="t('components.skill.actions.refresh')" :disabled="refreshing" @click="refresh">
        <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ spin: refreshing }">
          <path d="M20.5 8a8.5 8.5 0 10-2.38 7.41" fill="none" stroke="currentColor" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round" />
          <path d="M20.5 4v4h-4" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"
            stroke-linejoin="round" />
        </svg>
      </button>
      <button class="ghost-icon" :title="t('components.skill.actions.openFolder')"
        :data-tooltip="t('components.skill.actions.openFolder')" @click="handleOpenFolder">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" fill="none"
            stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
      <button class="ghost-icon" :title="t('components.skill.repos.open')"
        :data-tooltip="t('components.skill.repos.open')" @click="openRepoModal">
        <svg viewBox="0 0 24 24" aria-hidden="true">
          <path d="M5 5h14v6H5zM7 13h10v6H7z" fill="none" stroke="currentColor" stroke-width="1.5"
            stroke-linecap="round" stroke-linejoin="round" />
          <path d="M12 7.5v1M12 15.5v1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
    </div>

    <div class="contrib-page skill-page">
      <header class="skill-hero">
        <div class="skill-hero-text">
          <h1>{{ t('components.skill.hero.title') }}</h1>
          <p class="skill-lead">{{ t('components.skill.hero.lead') }}</p>
        </div>
      </header>

      <!-- Platform Tabs -->
      <div class="skill-platform-tabs">
        <button
          v-for="platform in platforms"
          :key="platform.value"
          :class="['skill-platform-tab', { active: activePlatform === platform.value }]"
          @click="switchPlatform(platform.value)"
        >
          {{ platform.label }}
        </button>
      </div>

      <section class="skill-list-section">
        <div v-if="loading" class="skill-empty">{{ t('components.skill.list.loading') }}</div>

        <template v-else>
          <!-- Project Skills Group -->
          <div v-if="projectSkills.length > 0" class="skill-group">
            <div class="skill-group-header">
              <h2 class="skill-group-title">
                {{ t('components.skill.groups.project') }} ({{ projectSkills.length }})
              </h2>
              <button
                type="button"
                class="ghost-icon sm"
                :title="t('components.skill.actions.openFolder')"
                @click="handleOpenFolderForLocation('project')"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" fill="none"
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </button>
            </div>
            <div class="skill-list installed-skills">
              <SkillCard
                v-for="skill in projectSkills"
                :key="skill.key"
                :skill="skill"
                :expanded="expandedSkills.has(skill.key)"
                :toggling="togglingSkill === skill.key"
                :uninstalling="processingSkill === uninstallProcessingKey(skill)"
                @toggle="handleToggle"
                @expand="toggleExpand"
                @uninstall="handleUninstall"
                @view="openGithub"
              />
            </div>
          </div>

          <!-- User Skills Group -->
          <div v-if="userSkills.length > 0" class="skill-group">
            <div class="skill-group-header">
              <h2 class="skill-group-title">
                {{ t('components.skill.groups.user') }} ({{ userSkills.length }})
              </h2>
              <button
                type="button"
                class="ghost-icon sm"
                :title="t('components.skill.actions.openFolder')"
                @click="handleOpenFolderForLocation('user')"
              >
                <svg viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" fill="none"
                    stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
              </button>
            </div>
            <div class="skill-list installed-skills">
              <SkillCard
                v-for="skill in userSkills"
                :key="skill.key"
                :skill="skill"
                :expanded="expandedSkills.has(skill.key)"
                :toggling="togglingSkill === skill.key"
                :uninstalling="processingSkill === uninstallProcessingKey(skill)"
                @toggle="handleToggle"
                @expand="toggleExpand"
                @uninstall="handleUninstall"
                @view="openGithub"
              />
            </div>
          </div>

          <!-- No Installed Skills Message -->
          <div v-if="projectSkills.length === 0 && userSkills.length === 0 && installedSkills.length === 0" class="skill-empty-installed">
            {{ t('components.skill.list.noInstalled') }}
          </div>

          <!-- Available Skills Group -->
          <div v-if="availableSkills.length > 0" class="skill-group">
            <div class="skill-group-header">
              <h2 class="skill-group-title">
                {{ t('components.skill.groups.available') }} ({{ availableSkills.length }})
              </h2>
            </div>
            <div class="skill-list">
              <article v-for="skill in availableSkills" :key="skill.key || skill.directory" class="skill-card available-card">
                <div class="skill-card-head">
                  <div>
                    <p class="skill-card-eyebrow">{{ skill.directory }}</p>
                    <h3>{{ skill.name }}</h3>
                  </div>
                  <div class="skill-card-actions">
                    <button type="button" class="ghost-icon sm" :title="t('components.skill.actions.view')"
                      :data-tooltip="t('components.skill.actions.view')" @click="openGithub(skill.readme_url)">
                      <svg viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M12 5h7v7M19 5l-9 9" fill="none" stroke="currentColor" stroke-width="1.6"
                          stroke-linecap="round" stroke-linejoin="round" />
                        <path d="M11 6H7a2 2 0 00-2 2v9a2 2 0 002 2h9a2 2 0 002-2v-4" fill="none" stroke="currentColor"
                          stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                      </svg>
                    </button>
                    <button type="button" class="ghost-icon sm"
                      :title="canInstallSkill(skill) ? t('components.skill.actions.install') : t('components.skill.list.missingRepo')"
                      :data-tooltip="canInstallSkill(skill) ? t('components.skill.actions.install') : t('components.skill.list.missingRepo')"
                      :disabled="isInstallingSkill(skill) || !canInstallSkill(skill)"
                      @click="openInstallModal(skill)">
                      <svg v-if="!isInstallingSkill(skill)" viewBox="0 0 24 24" aria-hidden="true">
                        <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"
                          stroke-linejoin="round" fill="none" />
                      </svg>
                      <span v-else class="skill-action-spinner" aria-hidden="true"></span>
                    </button>
                  </div>
                </div>
                <p class="skill-card-desc">
                  {{ skill.description || t('components.skill.list.noDescription') }}
                </p>
              </article>
            </div>
          </div>

          <!-- Empty State -->
          <div v-if="skills.length === 0" class="skill-empty">
            {{ t('components.skill.list.empty') }}
          </div>
        </template>

        <p v-if="skillsError" class="skill-error">{{ skillsError }}</p>
      </section>
    </div>

    <!-- Install Location Modal -->
    <BaseModal :open="installModalOpen" :title="t('components.skill.install.title')" @close="closeInstallModal">
      <div class="install-modal-content">
        <p class="install-modal-desc">
          {{ t('components.skill.install.desc', { name: installTarget?.name }) }}
        </p>

        <div class="install-location-options">
          <label class="install-option" :class="{ selected: installLocation === 'user' }">
            <input type="radio" v-model="installLocation" value="user" class="sr-only" />
            <div class="install-option-content">
              <svg viewBox="0 0 24 24" class="install-option-icon" aria-hidden="true">
                <path d="M20 21v-2a4 4 0 00-4-4H8a4 4 0 00-4 4v2M12 11a4 4 0 100-8 4 4 0 000 8z" fill="none"
                  stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <div>
                <p class="install-option-title">{{ t('components.skill.install.userLevel') }}</p>
                <p class="install-option-desc">~/.{{ activePlatform }}/skills/</p>
              </div>
            </div>
          </label>

          <label class="install-option" :class="{ selected: installLocation === 'project' }">
            <input type="radio" v-model="installLocation" value="project" class="sr-only" />
            <div class="install-option-content">
              <svg viewBox="0 0 24 24" class="install-option-icon" aria-hidden="true">
                <path d="M3 7v10a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-6l-2-2H5a2 2 0 00-2 2z" fill="none"
                  stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
              <div>
                <p class="install-option-title">{{ t('components.skill.install.projectLevel') }}</p>
                <p class="install-option-desc">./.{{ activePlatform }}/skills/</p>
                <p class="install-option-warning">{{ t('components.skill.install.gitWarning') }}</p>
              </div>
            </div>
          </label>
        </div>

        <div class="install-modal-actions">
          <button class="btn-secondary" @click="closeInstallModal">
            {{ t('common.cancel') }}
          </button>
          <button class="btn-primary" @click="confirmInstall" :disabled="installing">
            {{ installing ? t('components.skill.install.installing') : t('components.skill.install.confirm') }}
          </button>
        </div>
      </div>
    </BaseModal>

    <!-- Repository Modal -->
    <BaseModal :open="repoModalOpen" :title="t('components.skill.repos.title')" @close="closeRepoModal">
      <div class="skill-repo-section repo-modal-content">
        <p class="skill-repo-subtitle">{{ t('components.skill.repos.subtitle') }}</p>
        <form class="skill-repo-form" @submit.prevent="submitRepo">
          <div class="repo-input-field">
            <input v-model="repoForm.url" type="text" :placeholder="t('components.skill.repos.urlPlaceholder')"
              :disabled="repoBusy" />
          </div>
          <div class="repo-form-actions">
            <input v-model="repoForm.branch" type="text" :placeholder="t('components.skill.repos.branchPlaceholder')"
              :disabled="repoBusy" />
            <button class="ghost-icon" type="submit" :disabled="repoBusy" :title="t('components.skill.repos.addLabel')"
              :data-tooltip="t('components.skill.repos.addLabel')">
              <svg viewBox="0 0 24 24" aria-hidden="true" :class="{ spin: repoBusy }">
                <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"
                  stroke-linejoin="round" fill="none" />
              </svg>
            </button>
          </div>
        </form>
        <p v-if="repoError" class="skill-error">{{ repoError }}</p>
        <div class="skill-repo-list" :class="{ loading: repoLoading }">
          <p v-if="repoLoading" class="skill-empty">{{ t('components.skill.repos.loading') }}</p>
          <p v-else-if="!repoList.length" class="skill-empty">{{ t('components.skill.repos.empty') }}</p>
          <div v-else>
            <article v-for="repo in repoList" :key="repoKey(repo)" class="skill-repo-item">
              <div class="skill-repo-meta">
                <p class="repo-name">{{ repo.owner }}/{{ repo.name }}</p>
                <span class="repo-branch">{{ t('components.skill.repos.branchLabel', { branch: repo.branch }) }}</span>
              </div>
              <div class="skill-repo-actions">
                <button class="ghost-icon sm" type="button" :title="t('components.skill.repos.viewLabel')"
                  :data-tooltip="t('components.skill.repos.viewLabel')" @click="openRepoGithub(repo)">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 5h7v7M19 5l-9 9" fill="none" stroke="currentColor" stroke-width="1.6"
                      stroke-linecap="round" stroke-linejoin="round" />
                    <path d="M11 6H7a2 2 0 00-2 2v9a2 2 0 002 2h9a2 2 0 002-2v-4" fill="none" stroke="currentColor"
                      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
                <button class="ghost-icon sm danger" type="button" :title="t('components.skill.repos.removeLabel')"
                  :data-tooltip="t('components.skill.repos.removeLabel')" :disabled="repoBusy"
                  @click="removeRepo(repo)">
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M5 7h14M10 11v6M14 11v6M9 7V5h6v2" fill="none" stroke="currentColor" stroke-width="1.6"
                      stroke-linecap="round" stroke-linejoin="round" />
                    <path d="M6.5 7l-.5 12a2 2 0 002 2h8a2 2 0 002-2L17.5 7" fill="none" stroke="currentColor"
                      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                  </svg>
                </button>
              </div>
            </article>
          </div>
        </div>
      </div>
    </BaseModal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Browser } from '@wailsio/runtime'
import {
  fetchSkills,
  fetchSkillsForPlatform,
  installSkill,
  uninstallSkillEx,
  toggleSkill,
  getSkillContent,
  openSkillFolder,
  fetchSkillRepos,
  addSkillRepo,
  removeSkillRepo,
  type SkillSummary,
  type SkillRepoConfig
} from '../../services/skill'
import BaseModal from '../common/BaseModal.vue'
import SkillCard from './SkillCard.vue'

const router = useRouter()
const { t } = useI18n()

// Platform definitions (use computed for i18n reactivity)
const platforms = computed(() => [
  { value: 'claude' as const, label: t('components.skill.platform.claude') },
  { value: 'codex' as const, label: t('components.skill.platform.codex') }
])

// State
const activePlatform = ref<'claude' | 'codex'>('claude')
const skills = ref<SkillSummary[]>([])
const repoList = ref<SkillRepoConfig[]>([])
const loading = ref(false)
const repoLoading = ref(false)
const skillsError = ref('')
const repoError = ref('')
const processingSkill = ref('')
const togglingSkill = ref('')
const repoBusy = ref(false)
const repoForm = reactive({ url: '', branch: 'main' })
const repoModalOpen = ref(false)

// Install modal state
const installModalOpen = ref(false)
const installTarget = ref<SkillSummary | null>(null)
const installLocation = ref<'user' | 'project'>('user')
const installing = ref(false)

// Expanded skills
const expandedSkills = ref<Set<string>>(new Set())

const refreshing = computed(() => loading.value || repoLoading.value)

// Computed: Split skills by location
const installedSkills = computed(() =>
  skills.value.filter(s => s.installed)
)

const projectSkills = computed(() =>
  skills.value.filter(s => s.install_location === 'project' && s.installed)
)

const userSkills = computed(() =>
  skills.value.filter(s => s.install_location === 'user' && s.installed)
)

const availableSkills = computed(() =>
  skills.value.filter(s => !s.installed)
)

// Skill identity helpers
const skillIdentity = (skill: SkillSummary) =>
  skill.key || `${(skill.repo_owner ?? 'local').toLowerCase()}:${skill.directory.toLowerCase()}`

const installProcessingKey = (skill: SkillSummary) => `install:${skillIdentity(skill)}`
const uninstallProcessingKey = (skill: SkillSummary) => `uninstall:${skillIdentity(skill)}`

const isInstallingSkill = (skill: SkillSummary) => processingSkill.value === installProcessingKey(skill)
const canInstallSkill = (skill: SkillSummary) => Boolean(skill.repo_owner && skill.repo_name)

// Platform switching
const switchPlatform = async (platform: 'claude' | 'codex') => {
  activePlatform.value = platform
  await loadSkillsForPlatform()
}

// Load skills for current platform
const loadSkillsForPlatform = async () => {
  loading.value = true
  skillsError.value = ''
  try {
    // Load installed skills for this platform (has correct install_location)
    const installed = await fetchSkillsForPlatform(activePlatform.value)
    // Also load available skills from repos
    const available = await fetchSkills()

    // FIX: Only keep repo skills that can be installed (have repo info)
    // Force installed=false to avoid "gap" where skills fall into neither group
    const availableClean = available
      .filter(s => s.repo_owner && s.repo_name)  // Only installable repo skills
      .map(s => ({
        ...s,
        installed: false,  // Force to false - actual status from fetchSkillsForPlatform
        install_location: '' as const,
        platform: '' as const
      }))

    // Merge: installed skills take precedence by directory name
    const installedDirs = new Set(installed.map(s => s.directory.toLowerCase()))
    const filtered = availableClean.filter(s => !installedDirs.has(s.directory.toLowerCase()))

    skills.value = [...installed, ...filtered]
  } catch (error) {
    console.error('failed to load skills', error)
    skillsError.value = t('components.skill.list.error')
  } finally {
    loading.value = false
    processingSkill.value = ''
  }
}

const loadRepos = async () => {
  repoLoading.value = true
  repoError.value = ''
  try {
    repoList.value = await fetchSkillRepos()
  } catch (error) {
    console.error('failed to load skill repos', error)
    repoError.value = t('components.skill.repos.loadError')
  } finally {
    repoLoading.value = false
  }
}

const refresh = () => {
  void Promise.all([loadRepos(), loadSkillsForPlatform()])
}

// Toggle skill enabled status
const handleToggle = async (skill: SkillSummary, enabled: boolean) => {
  togglingSkill.value = skill.key
  try {
    await toggleSkill(
      skill.directory,
      skill.platform || activePlatform.value,
      skill.install_location || 'user',
      enabled
    )
    // Update local state
    const target = skills.value.find(s => s.key === skill.key)
    if (target) {
      target.enabled = enabled
    }
  } catch (error) {
    console.error('failed to toggle skill', error)
    skillsError.value = t('components.skill.actions.toggleError')
  } finally {
    togglingSkill.value = ''
  }
}

// Toggle content expansion
const toggleExpand = async (skill: SkillSummary) => {
  const key = skill.key
  if (expandedSkills.value.has(key)) {
    expandedSkills.value.delete(key)
  } else {
    expandedSkills.value.add(key)
  }
}

// Open skill folder (default: user location)
const handleOpenFolder = async () => {
  try {
    await openSkillFolder(activePlatform.value, 'user')
  } catch (error) {
    console.error('failed to open folder', error)
  }
}

// Open skill folder for specific location
const handleOpenFolderForLocation = async (location: 'user' | 'project') => {
  try {
    await openSkillFolder(activePlatform.value, location)
  } catch (error) {
    console.error('failed to open folder', error)
  }
}

// Install modal
const openInstallModal = (skill: SkillSummary) => {
  installTarget.value = skill
  installLocation.value = 'user'
  installModalOpen.value = true
}

const closeInstallModal = () => {
  installModalOpen.value = false
  installTarget.value = null
}

const confirmInstall = async () => {
  if (!installTarget.value || !canInstallSkill(installTarget.value)) return

  installing.value = true
  processingSkill.value = installProcessingKey(installTarget.value)

  try {
    await installSkill({
      directory: installTarget.value.directory,
      repo_owner: installTarget.value.repo_owner,
      repo_name: installTarget.value.repo_name,
      repo_branch: installTarget.value.repo_branch,
      platform: activePlatform.value,
      location: installLocation.value
    })
    skillsError.value = ''
    closeInstallModal()
    await loadSkillsForPlatform()
  } catch (error) {
    console.error('failed to install skill', error)
    skillsError.value = t('components.skill.actions.installError', { name: installTarget.value.name })
  } finally {
    installing.value = false
    processingSkill.value = ''
  }
}

// Uninstall
const handleUninstall = async (skill: SkillSummary) => {
  processingSkill.value = uninstallProcessingKey(skill)
  try {
    await uninstallSkillEx(
      skill.directory,
      skill.platform || activePlatform.value,
      skill.install_location || 'user'
    )
    skillsError.value = ''
    await loadSkillsForPlatform()
  } catch (error) {
    console.error('failed to uninstall skill', error)
    skillsError.value = t('components.skill.actions.uninstallError', { name: skill.name })
  } finally {
    processingSkill.value = ''
  }
}

// Navigation
const goHome = () => {
  router.push('/')
}

const openExternal = (target: string) => {
  if (!target) return
  Browser.OpenURL(target).catch(() => {
    console.error('failed to open link', target)
  })
}

const openGithub = (url: string) => {
  if (!url) return
  openExternal(url)
}

// Repository modal
const openRepoModal = () => {
  repoModalOpen.value = true
  if (!repoList.value.length && !repoLoading.value) {
    void loadRepos()
  }
}

const closeRepoModal = () => {
  repoModalOpen.value = false
}

const repoKey = (repo: SkillRepoConfig) => `${repo.owner}/${repo.name}`

const parseRepoInput = (value: string) => {
  let input = value.trim()
  if (!input) return null
  input = input.replace(/^https?:\/\/(www\.)?github\.com\//i, '')
  input = input.replace(/\.git$/i, '')
  const parts = input.split('/')
  if (parts.length < 2) return null
  const owner = parts[0]
  const name = parts[1]
  if (!owner || !name) return null
  return { owner, name }
}

const submitRepo = async () => {
  const parsed = parseRepoInput(repoForm.url)
  if (!parsed) {
    repoError.value = t('components.skill.repos.formError')
    return
  }
  repoBusy.value = true
  repoError.value = ''
  try {
    repoList.value = await addSkillRepo({
      owner: parsed.owner,
      name: parsed.name,
      branch: repoForm.branch || 'main',
      enabled: true
    })
    repoForm.url = ''
    repoForm.branch = 'main'
    await loadSkillsForPlatform()
  } catch (error) {
    console.error('failed to add skill repo', error)
    repoError.value = t('components.skill.repos.addError')
  } finally {
    repoBusy.value = false
  }
}

const removeRepo = async (repo: SkillRepoConfig) => {
  repoBusy.value = true
  repoError.value = ''
  try {
    repoList.value = await removeSkillRepo(repo.owner, repo.name)
    await loadSkillsForPlatform()
  } catch (error) {
    console.error('failed to remove skill repo', error)
    repoError.value = t('components.skill.repos.removeError')
  } finally {
    repoBusy.value = false
  }
}

const openRepoGithub = (repo: SkillRepoConfig) => {
  if (!repo?.owner || !repo?.name) return
  openExternal(`https://github.com/${repo.owner}/${repo.name}`)
}

onMounted(() => {
  void Promise.all([loadRepos(), loadSkillsForPlatform()])
})
</script>

<style scoped>
.skill-page {
  gap: 32px;
  color: var(--mac-text);
}

/* Platform Tabs */
.skill-platform-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 24px;
  border-bottom: 1px solid var(--mac-border);
  padding-bottom: 12px;
}

.skill-platform-tab {
  padding: 8px 16px;
  border: 1px solid var(--mac-border);
  border-radius: 8px;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.2s ease;
}

.skill-platform-tab:hover {
  background: var(--mac-surface);
  color: var(--mac-text);
}

.skill-platform-tab.active {
  background: var(--mac-accent);
  color: white;
  border-color: var(--mac-accent);
}

/* Skill Groups */
.skill-group {
  margin-bottom: 32px;
}

.skill-group-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.skill-group-title {
  font-size: 1rem;
  font-weight: 600;
  margin: 0;
  color: var(--mac-text-secondary);
  display: flex;
  align-items: center;
  gap: 8px;
}

.skill-group-title::before {
  content: '';
  display: inline-block;
  width: 4px;
  height: 16px;
  background: var(--mac-accent);
  border-radius: 2px;
}

.skill-empty-installed {
  text-align: center;
  color: var(--mac-text-secondary);
  padding: 24px;
  margin-bottom: 24px;
}

/* Skill List */
.skill-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.installed-skills {
  gap: 16px;
}

/* Available Skill Card */
.skill-card.available-card {
  background: var(--mac-surface-strong); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 90%, transparent);
  border: 1px solid var(--mac-border);
  border-radius: 16px;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.skill-card-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.skill-card-eyebrow {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.15em;
  color: var(--mac-text-secondary);
  margin-bottom: 4px;
}

.skill-card h3 {
  font-size: 0.95rem;
  margin: 0;
}

.skill-card-desc {
  color: var(--mac-text-secondary);
  font-size: 0.85rem;
  line-height: 1.4;
}

.skill-card-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

/* Install Modal */
.install-modal-content {
  min-width: min(400px, 80vw);
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.install-modal-desc {
  color: var(--mac-text-secondary);
  font-size: 0.95rem;
}

.install-location-options {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.install-option {
  display: block;
  cursor: pointer;
}

.install-option-content {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding: 16px;
  border: 2px solid var(--mac-border);
  border-radius: 12px;
  transition: all 0.2s ease;
}

.install-option:hover .install-option-content {
  border-color: var(--mac-accent);
}

.install-option.selected .install-option-content {
  border-color: var(--mac-accent);
  background: rgba(10, 132, 255, 0.1); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-accent) 10%, transparent);
}

.install-option-icon {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  color: var(--mac-text-secondary);
}

.install-option.selected .install-option-icon {
  color: var(--mac-accent);
}

.install-option-title {
  font-weight: 600;
  margin: 0 0 4px;
}

.install-option-desc {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
  margin: 0;
  font-family: monospace;
}

.install-option-warning {
  font-size: 0.8rem;
  color: #f59e0b;
  margin: 8px 0 0;
}

.install-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  padding-top: 8px;
  border-top: 1px solid var(--mac-border);
}

.btn-primary,
.btn-secondary {
  padding: 8px 20px;
  border-radius: 8px;
  font-weight: 500;
  font-size: 0.9rem;
  cursor: pointer;
  transition: all 0.2s ease;
}

.btn-primary {
  background: var(--mac-accent);
  color: white;
  border: none;
}

.btn-primary:hover {
  opacity: 0.9;
}

.btn-primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-secondary {
  background: transparent;
  color: var(--mac-text);
  border: 1px solid var(--mac-border);
}

.btn-secondary:hover {
  background: var(--mac-surface);
}

/* Repository Section (reused from original) */
.skill-repo-section {
  border: 1px solid var(--mac-border);
  border-radius: 20px;
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  background: var(--mac-surface-strong); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 90%, transparent);
}

.skill-repo-subtitle {
  margin: 0 0 12px;
  color: var(--mac-text-secondary);
  font-size: 0.95rem;
}

.skill-repo-form {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  width: 100%;
}

.repo-input-field {
  flex: 1;
  min-width: 220px;
}

.skill-repo-form input {
  border: 1px solid var(--mac-border);
  border-radius: 10px;
  padding: 8px 12px;
  background: var(--mac-surface);
  color: var(--mac-text);
  font-size: 0.9rem;
}

.repo-input-field input {
  width: 100%;
}

.repo-form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  align-items: center;
}

.repo-form-actions input {
  width: 160px;
}

.skill-repo-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.skill-repo-list.loading {
  opacity: 0.7;
}

.skill-repo-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 18px;
  border: 1px solid var(--mac-border);
  border-radius: 12px;
  background: var(--mac-surface-strong); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 80%, transparent);
  gap: 16px;
  margin: 0 0 8px;
}

.skill-repo-item:last-child {
  margin-bottom: 0;
}

.skill-repo-meta {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.skill-repo-meta .repo-name {
  margin: 0;
  font-weight: 600;
}

.skill-repo-meta .repo-branch {
  font-size: 0.85rem;
  color: var(--mac-text-secondary);
}

.skill-repo-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}

.repo-modal-content {
  min-width: min(600px, 80vw);
}

/* Common */
.skill-hero {
  margin: 12px 0 12px;
}

.skill-lead {
  color: var(--mac-text-secondary);
  font-size: 0.95rem;
  line-height: 1.5;
}

.skill-hero h1 {
  font-size: clamp(26px, 3vw, 34px);
  margin-bottom: 8px;
}

.skill-list-section {
  margin-top: 16px;
}

.skill-empty {
  margin-top: 32px;
  color: var(--mac-text-secondary);
  text-align: center;
}

.skill-repo-list .skill-empty {
  margin-top: 0;
}

.skill-error {
  color: #f87171;
  margin-top: 16px;
}

.ghost-icon svg.spin {
  animation: skill-spin 1s linear infinite;
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

.ghost-icon:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.ghost-icon.danger {
  color: #ef4444;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border-width: 0;
}

html.dark .skill-card.available-card {
  background: var(--mac-surface); /* fallback for old WebKit */
  background: color-mix(in srgb, var(--mac-surface) 70%, transparent);
}

@media (max-width: 768px) {
  .skill-hero {
    flex-direction: column;
  }

  .skill-platform-tabs {
    flex-wrap: wrap;
  }
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
