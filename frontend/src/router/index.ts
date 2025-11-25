import { createRouter, createWebHashHistory } from 'vue-router'
import MainPage from '../components/Main/Index.vue'
import LogsPage from '../components/Logs/Index.vue'
import GeneralPage from '../components/General/Index.vue'
import McpPage from '../components/Mcp/index.vue'
import SkillPage from '../components/Skill/Index.vue'
import PromptsPage from '../components/Prompts/Index.vue'
import SpeedTestPage from '../components/SpeedTest/Index.vue'
import EnvCheckPage from '../components/EnvCheck/Index.vue'

const routes = [
  { path: '/', component: MainPage },
  { path: '/prompts', component: PromptsPage },
  { path: '/mcp', component: McpPage },
  { path: '/skill', component: SkillPage },
  { path: '/speedtest', component: SpeedTestPage },
  { path: '/env', component: EnvCheckPage },
  { path: '/logs', component: LogsPage },
  { path: '/settings', component: GeneralPage },
]

export default createRouter({
  history: createWebHashHistory(), // Use createWebHashHistory for hash-based routing
  routes
})
