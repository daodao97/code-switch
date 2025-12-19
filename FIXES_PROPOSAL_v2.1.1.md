# v2.1.1 问题修复方案

## 问题清单

基于用户反馈，发现以下 4 个问题需要修复：

1. ❌ 可用性页面看不到"编辑配置"按钮
2. ❌ "全部检测"按钮对比度太弱，不容易发现
3. ❌ MCP "粘贴 JSON" tab 点击后空白
4. ❌ MCP 表单配置方式填写参数后不生效

---

## 问题 1：可用性页面编辑按钮显示逻辑

### 现状分析

**代码位置**：`frontend/src/components/Availability/Index.vue:342-349`

**当前实现**：
```vue
<button
  v-if="timeline.availabilityMonitorEnabled"
  @click="editConfig(platform, timeline)"
  class="..."
>
  {{ t('availability.editConfig') }}
</button>
```

**问题**：
- 按钮仅在 `availabilityMonitorEnabled = true` 时显示
- 用户未启用监控时看不到按钮
- 不符合"先配置后启用"的操作习惯

### 解决方案（推荐）

**方案 B：始终显示，未启用时置灰**

**修改代码**：
```vue
<button
  @click="editConfig(platform, timeline)"
  :disabled="!timeline.availabilityMonitorEnabled"
  :title="timeline.availabilityMonitorEnabled ? '' : t('availability.enableToMonitor')"
  class="px-3 py-1 text-sm rounded-lg transition-colors bg-[var(--mac-accent)] text-white hover:opacity-90 disabled:bg-gray-300 disabled:text-gray-500 disabled:cursor-not-allowed dark:disabled:bg-gray-700 dark:disabled:text-gray-400"
>
  {{ t('availability.editConfig') }}
</button>
```

**优点**：
- ✅ 用户能看到配置入口
- ✅ 置灰状态清晰提示需要先启用
- ✅ 不改变启用逻辑，保持安全性

**国际化新增**：
```json
// zh.json
"availability": {
  "enableToMonitor": "请先启用可用性监控"
}

// en.json
"availability": {
  "enableToMonitor": "Please enable availability monitoring first"
}
```

---

## 问题 2：全部检测按钮对比度优化

### 现状分析

**代码位置**：`frontend/src/components/Availability/Index.vue:242-247`

**当前实现**：
```vue
<button
  @click="refreshAll"
  :disabled="refreshing"
  class="px-4 py-2 bg-[var(--mac-accent)] text-white rounded-lg hover:opacity-90 transition-opacity disabled:opacity-50"
>
  ...
</button>
```

**问题**：
- 使用主题变量 `--mac-accent`，在某些主题下对比度不足
- 按钮尺寸和样式不够突出

### 解决方案（推荐）

**方案：增强对比度 + 视觉层次**

**修改代码**：
```vue
<button
  @click="refreshAll"
  :disabled="refreshing"
  class="px-6 py-2.5 text-base font-semibold bg-blue-600 hover:bg-blue-700 text-white rounded-xl shadow-md hover:shadow-lg transition-all disabled:opacity-50 disabled:cursor-not-allowed"
>
  <span v-if="refreshing">🔄 {{ t('availability.refreshing') }}</span>
  <span v-else>⚡ {{ t('availability.refreshAll') }}</span>
</button>
```

**改进点**：
- ✅ 使用固定的 `bg-blue-600`，对比度强
- ✅ 增加 padding 和字号，更显眼
- ✅ 添加阴影效果，增强层次感
- ✅ 添加 emoji 图标，视觉引导
- ✅ hover 时阴影增强，交互反馈明确

---

## 问题 3：MCP 粘贴 JSON tab 空白

### 现状分析

**代码位置**：`frontend/src/components/Mcp/index.vue`
- Tab 按钮：第 187-203 行
- 内容区域：第 301-377 行

**当前实现**：
```vue
<!-- Tab 按钮区域 -->
<div v-if="!modalState.editingName" class="...">
  <button @click="switchModalMode('form')">表单配置</button>
  <button @click="switchModalMode('json')">粘贴 JSON</button>
</div>

<!-- JSON 内容区域 -->
<div v-if="modalMode === 'json'">
  <textarea v-model="jsonInput" ...></textarea>
</div>
```

**问题**：
- Tab 区域有 `v-if="!modalState.editingName"` 限制
- 编辑模式下整个 Tab 不显示
- 用户看到的是完全空白

### 解决方案（推荐）

**方案：移除编辑模式限制，允许粘贴 JSON 覆盖配置**

**修改代码**：
```vue
<!-- 移除 v-if 限制 -->
<div class="...">
  <button
    @click="switchModalMode('form')"
    :class="modalMode === 'form' ? 'active' : ''"
  >
    {{ t('mcp.form.tabForm') }}
  </button>
  <button
    @click="switchModalMode('json')"
    :class="modalMode === 'json' ? 'active' : ''"
  >
    {{ t('mcp.form.tabJson') }}
  </button>
</div>

<!-- JSON 内容保持不变 -->
<div v-if="modalMode === 'json'">
  <!-- 添加说明 -->
  <p class="text-sm text-[var(--mac-text-secondary)] mb-2">
    {{ t('mcp.form.jsonHint') }}
  </p>
  <textarea v-model="jsonInput" ...></textarea>
</div>
```

**新增国际化**：
```json
{
  "mcp": {
    "form": {
      "jsonHint": "粘贴 MCP 服务器 JSON 配置，将覆盖当前配置"
    }
  }
}
```

**优点**：
- ✅ 编辑模式也能粘贴 JSON
- ✅ 提供清晰的说明
- ✅ 用户体验更好

---

## 问题 4：MCP 表单配置不生效

### 现状分析

**后端代码**：`services/mcpservice.go:185-192`
```go
// 如果有占位符未填写，清空平台启用状态（静默失败）
if HasPlaceholders(params) || HasPlaceholders(env) {
    server.EnableClaude = false
    server.EnableCodex = false
}
```

**前端代码**：`frontend/src/components/Mcp/index.vue:785-846`
- 保存时未校验平台是否勾选
- 未校验占位符是否填写
- 静默失败，用户无感知

### 解决方案（推荐）

**方案：前端强校验 + 后端明确错误**

#### 前端修改

**位置**：`frontend/src/components/Mcp/index.vue` 的 `submitMcpForm` 方法

**在保存前添加校验**：
```ts
const submitMcpForm = async () => {
  // 1. 校验至少勾选一个平台
  if (!platformState.enableClaude && !platformState.enableCodex) {
    // 显示错误提示
    showError(t('mcp.form.errors.noPlatformSelected'))
    return
  }

  // 2. 校验占位符是否填写完整
  if (formMissingPlaceholders.value) {
    showError(t('mcp.form.errors.missingPlaceholders'))
    // 高亮未填写的字段
    highlightMissingFields()
    return
  }

  // 3. 继续原有的保存逻辑
  try {
    await saveMcpServers(...)
    showSuccess(t('mcp.form.saveSuccess'))
  } catch (error) {
    showError(t('mcp.form.saveFailed'))
  }
}
```

#### 后端修改

**位置**：`services/mcpservice.go:185-192`

**返回明确错误**：
```go
// 校验占位符
if HasPlaceholders(params) || HasPlaceholders(env) {
    return fmt.Errorf("配置包含未填写的占位符: %s",
        detectPlaceholders(params, env))
}

// 校验至少启用一个平台
if !server.EnableClaude && !server.EnableCodex {
    return fmt.Errorf("请至少勾选一个平台（Claude 或 Codex）")
}
```

#### 国际化新增

```json
// zh.json
{
  "mcp": {
    "form": {
      "errors": {
        "noPlatformSelected": "请至少勾选一个平台（Claude Code 或 Codex）",
        "missingPlaceholders": "请填写所有占位符（{var} 格式的字段）"
      },
      "saveSuccess": "MCP 配置已保存并同步",
      "saveFailed": "保存失败，请检查配置"
    }
  }
}

// en.json
{
  "mcp": {
    "form": {
      "errors": {
        "noPlatformSelected": "Please select at least one platform (Claude Code or Codex)",
        "missingPlaceholders": "Please fill in all placeholders ({var} format fields)"
      },
      "saveSuccess": "MCP config saved and synced",
      "saveFailed": "Save failed, please check your config"
    }
  }
}
```

**优点**：
- ✅ 用户立即知道哪里出错
- ✅ 不会静默失败
- ✅ 前后端双重校验，更安全
- ✅ 错误信息明确，易于修复

---

## 方案对比总结

| 问题 | 推荐方案 | 理由 | 工作量 |
|------|---------|------|--------|
| 1. 编辑按钮 | 方案 B（始终显示+置灰） | 用户体验好，符合直觉 | 小 |
| 2. 按钮对比度 | 渐进增强（固定色+阴影） | 既显眼又不过分 | 小 |
| 3. JSON tab | 移除编辑限制 | 增强灵活性 | 极小 |
| 4. MCP 校验 | 前后端双重校验 | 彻底解决静默失败 | 中 |

---

## 实施优先级

### P0（必须修复）
1. ✅ 问题 4：MCP 校验（影响核心功能）
2. ✅ 问题 1：编辑按钮逻辑（影响用户体验）

### P1（强烈建议）
3. ✅ 问题 2：按钮对比度（可见性问题）
4. ✅ 问题 3：JSON tab（功能缺失）

---

## 需要你审核的点

### 1. 编辑按钮方案
- **我和 codex 的共识**：始终显示，未启用时置灰
- **是否同意**？还是有其他想法？

### 2. 按钮样式方案
- **codex 建议**：固定 `bg-blue-600` 颜色
- **我建议**：渐变 + 阴影 + emoji
- **你更喜欢哪个**？还是有第三种方案？

### 3. MCP JSON tab 方案
- **方案**：移除编辑模式限制
- **风险**：编辑时粘贴 JSON 会覆盖现有配置
- **是否接受**？还是只允许新建时使用？

### 4. MCP 校验方案
- **前端校验 + 后端错误返回**
- **需要实现**：错误提示组件（toast 或 alert）
- **是否同意这个方向**？

---

## 请审核并指示

1. **哪些方案需要调整**？
2. **是否还有其他要求**？
3. **是否同意开始实施**？

审核通过后，我将立即实施所有修复。
