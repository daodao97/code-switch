<script setup lang="ts">
import { computed, ref, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

// 侧边栏收起状态
const SIDEBAR_COLLAPSED_KEY = 'sidebar-collapsed'
const VISITED_PAGES_KEY = 'visited-pages'
const isCollapsed = ref(false)
const visitedPages = ref<Set<string>>(new Set())

onMounted(() => {
  // 加载侧边栏状态
  const saved = localStorage.getItem(SIDEBAR_COLLAPSED_KEY)
  if (saved !== null) {
    isCollapsed.value = saved === 'true'
  }
  // 加载已访问页面
  const visitedJson = localStorage.getItem(VISITED_PAGES_KEY)
  if (visitedJson) {
    try {
      visitedPages.value = new Set(JSON.parse(visitedJson))
    } catch {
      visitedPages.value = new Set()
    }
  }
  // 标记当前页面为已访问
  markAsVisited(route.path)
})

// 监听路由变化，标记为已访问
watch(() => route.path, (newPath) => {
  markAsVisited(newPath)
})

function markAsVisited(path: string) {
  if (!visitedPages.value.has(path)) {
    visitedPages.value.add(path)
    localStorage.setItem(VISITED_PAGES_KEY, JSON.stringify([...visitedPages.value]))
  }
}

// 判断是否显示 NEW 徽章（仅在未访问时显示）
function shouldShowNew(item: NavItem): boolean {
  return item.isNew === true && !visitedPages.value.has(item.path)
}

const toggleCollapse = () => {
  isCollapsed.value = !isCollapsed.value
  localStorage.setItem(SIDEBAR_COLLAPSED_KEY, String(isCollapsed.value))
}

interface NavItem {
  path: string
  icon: string
  labelKey: string
  isNew?: boolean
}

const navItems: NavItem[] = [
  { path: '/', icon: 'home', labelKey: 'sidebar.home' },
  { path: '/prompts', icon: 'file-text', labelKey: 'sidebar.prompts', isNew: true },
  { path: '/mcp', icon: 'plug', labelKey: 'sidebar.mcp' },
  { path: '/skill', icon: 'tool', labelKey: 'sidebar.skill' },
  { path: '/speedtest', icon: 'zap', labelKey: 'sidebar.speedtest', isNew: true },
  { path: '/env', icon: 'search', labelKey: 'sidebar.env', isNew: true },
  { path: '/logs', icon: 'bar-chart', labelKey: 'sidebar.logs' },
  { path: '/settings', icon: 'settings', labelKey: 'sidebar.settings' },
]

const currentPath = computed(() => route.path)

const navigate = (path: string) => {
  router.push(path)
}
</script>

<template>
  <nav class="mac-sidebar" :class="{ collapsed: isCollapsed }">
    <div class="sidebar-header">
      <span class="sidebar-title" v-if="!isCollapsed">Code Switch</span>
      <button class="collapse-btn" @click="toggleCollapse" :title="isCollapsed ? 'Expand' : 'Collapse'">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline v-if="isCollapsed" points="9 18 15 12 9 6"></polyline>
          <polyline v-else points="15 18 9 12 15 6"></polyline>
        </svg>
      </button>
    </div>

    <div class="nav-list">
      <button
        v-for="item in navItems"
        :key="item.path"
        class="nav-item"
        :class="{ active: currentPath === item.path }"
        :title="isCollapsed ? t(item.labelKey) : ''"
        @click="navigate(item.path)"
      >
        <!-- Home -->
        <svg v-if="item.icon === 'home'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"></path>
          <polyline points="9 22 9 12 15 12 15 22"></polyline>
        </svg>

        <!-- File Text -->
        <svg v-else-if="item.icon === 'file-text'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
          <polyline points="14 2 14 8 20 8"></polyline>
          <line x1="16" y1="13" x2="8" y2="13"></line>
          <line x1="16" y1="17" x2="8" y2="17"></line>
          <polyline points="10 9 9 9 8 9"></polyline>
        </svg>

        <!-- Plug -->
        <svg v-else-if="item.icon === 'plug'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M12 22v-5"></path>
          <path d="M9 8V2"></path>
          <path d="M15 8V2"></path>
          <path d="M18 8v5a6 6 0 0 1-12 0V8h12z"></path>
        </svg>

        <!-- Tool -->
        <svg v-else-if="item.icon === 'tool'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"></path>
        </svg>

        <!-- Zap -->
        <svg v-else-if="item.icon === 'zap'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"></polygon>
        </svg>

        <!-- Search -->
        <svg v-else-if="item.icon === 'search'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="11" cy="11" r="8"></circle>
          <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
        </svg>

        <!-- Bar Chart -->
        <svg v-else-if="item.icon === 'bar-chart'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <line x1="12" y1="20" x2="12" y2="10"></line>
          <line x1="18" y1="20" x2="18" y2="4"></line>
          <line x1="6" y1="20" x2="6" y2="16"></line>
        </svg>

        <!-- Settings -->
        <svg v-else-if="item.icon === 'settings'" class="nav-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <circle cx="12" cy="12" r="3"></circle>
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
        </svg>

        <span class="nav-label" v-if="!isCollapsed">{{ t(item.labelKey) }}</span>
        <span v-if="shouldShowNew(item) && !isCollapsed" class="new-badge">NEW</span>
      </button>
    </div>

    <div class="sidebar-footer" v-if="!isCollapsed">
      <span class="version">v1.1.11</span>
    </div>
  </nav>
</template>

<style scoped>
.mac-sidebar {
  width: 200px;
  min-width: 200px;
  background: var(--mac-surface);
  border-right: 1px solid var(--mac-border);
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  transition: width 0.2s ease, min-width 0.2s ease;
}

.mac-sidebar.collapsed {
  width: 56px;
  min-width: 56px;
}

.sidebar-header {
  padding: 20px 16px 16px;
  border-bottom: 1px solid var(--mac-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.mac-sidebar.collapsed .sidebar-header {
  padding: 16px 8px;
  justify-content: center;
}

.sidebar-title {
  font-size: 1.1rem;
  font-weight: 700;
  color: var(--mac-text);
  letter-spacing: -0.02em;
  white-space: nowrap;
  overflow: hidden;
}

.collapse-btn {
  width: 28px;
  height: 28px;
  border: none;
  background: transparent;
  border-radius: 6px;
  color: var(--mac-text-secondary);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
  flex-shrink: 0;
}

.collapse-btn:hover {
  background: rgba(15, 23, 42, 0.06);
  color: var(--mac-text);
}

html.dark .collapse-btn:hover {
  background: rgba(255, 255, 255, 0.08);
}

.collapse-btn svg {
  width: 16px;
  height: 16px;
}

.nav-list {
  flex: 1;
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow-y: auto;
}

.mac-sidebar.collapsed .nav-list {
  padding: 12px 4px;
  align-items: center;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 10px;
  border: none;
  background: transparent;
  color: var(--mac-text-secondary);
  font-size: 0.9rem;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  width: 100%;
  text-align: left;
}

.mac-sidebar.collapsed .nav-item {
  padding: 10px;
  justify-content: center;
}

.nav-item:hover {
  background: rgba(15, 23, 42, 0.06);
  color: var(--mac-text);
}

html.dark .nav-item:hover {
  background: rgba(255, 255, 255, 0.08);
}

.nav-item.active {
  background: var(--mac-accent);
  color: #fff;
}

.nav-item.active:hover {
  background: var(--mac-accent);
  color: #fff;
}

.nav-icon {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
}

.nav-label {
  flex: 1;
}

.new-badge {
  font-size: 0.6rem;
  font-weight: 700;
  padding: 2px 5px;
  border-radius: 4px;
  background: rgba(16, 185, 129, 0.15);
  color: #10b981;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.nav-item.active .new-badge {
  background: rgba(255, 255, 255, 0.2);
  color: #fff;
}

.sidebar-footer {
  padding: 12px 16px;
  border-top: 1px solid var(--mac-border);
}

.version {
  font-size: 0.75rem;
  color: var(--mac-text-secondary);
  opacity: 0.6;
}
</style>
