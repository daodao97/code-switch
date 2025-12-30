<script setup lang="ts">
import { RouterView } from 'vue-router'
import { onMounted } from 'vue'
import Sidebar from './components/Sidebar.vue'
import UpdateNotification from './components/common/UpdateNotification.vue'

const applyTheme = () => {
  const userTheme = localStorage.getItem('theme')
  const systemPrefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches

  const isDark = userTheme === 'dark' || (!userTheme && systemPrefersDark)

  document.documentElement.classList.toggle('dark', isDark)
}

onMounted(() => {
  applyTheme()

  // 可监听系统主题变化自动更新（可选）
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    applyTheme()
  })
})
</script>

<template>
  <div class="app-layout">
    <Sidebar />
    <main class="main-content">
      <RouterView v-slot="{ Component }">
        <keep-alive>
          <component :is="Component" />
        </keep-alive>
      </RouterView>
    </main>
    <!-- 更新通知弹窗 -->
    <UpdateNotification />
  </div>
</template>

<style scoped>
.app-layout {
  display: flex;
  height: 100vh;
  width: 100vw;
  overflow: hidden;
}

.main-content {
  flex: 1;
  overflow-y: auto;
  background: var(--mac-bg);
}
</style>
