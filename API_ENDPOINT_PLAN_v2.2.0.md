# API 端点配置功能方案（v2.2.0）

经过与 codex 的深入讨论，我们达成以下方案共识：

---

## 📋 问题分析

### 用户遇到的问题

**现象**：
```
Provider 阿萨 映射模型：claude-haiku-4-5-20251001 -> glm-4.6
错误：404 Not Found，路径：/v1/messages
```

**根本原因**：
- 代码根据平台（claude）硬编码使用 `/v1/messages` 端点
- 但 GLM 模型需要使用 `/v1/chat/completions` 或 `/api/paas/v4/chat/completions`
- 模型映射只改模型名，不改端点
- 导致 404 错误

---

## 🎯 解决方案（与 codex 达成共识）

### 采用方案 A：添加可选的 apiEndpoint 字段

**核心设计**：
1. ✅ 在 Provider 结构体添加 `apiEndpoint` 字段
2. ✅ 用户可选填，留空使用平台默认
3. ✅ 前端使用"下拉常用 + 自定义输入"混合方式
4. ✅ 轻量校验，不过度复杂

---

## 📊 与 codex 的争论结果

### 争论点 1：字段独立性

**我的观点**：应该独立于 `availabilityConfig.testEndpoint`
- `availabilityConfig.testEndpoint` → 健康检查用
- `apiEndpoint` → 生产请求用

**codex 观点**：完全同意，必须分离

**最终共识**：✅ **新增独立的 apiEndpoint 字段**

---

### 争论点 2：前端 UI 设计

**codex 推荐**：下拉 + 自定义输入（选项 2）

**UI 设计**：
```
┌─────────────────────────────────────┐
│ API 端点（可选）                     │
│ [下拉选择 ▼]  [或输入自定义]         │
│                                     │
│ 选项：                               │
│ • /v1/messages (Anthropic)         │
│ • /v1/chat/completions (OpenAI)    │
│ • /api/paas/v4/chat/completions (GLM)│
│ • 自定义...                         │
│                                     │
│ 💡 留空使用平台默认                  │
│    claude: /v1/messages            │
│    codex: /responses               │
└─────────────────────────────────────┘
```

**位置**：Provider 编辑弹窗的基础配置区域，API URL 下方

**最终共识**：✅ **下拉 + 自定义混合方式**

---

### 争论点 3：端点优先级

**我的方案 A**：provider.apiEndpoint > 平台默认

**codex 观点**：同意方案 A，不引入模型推断（避免误判）

**最终共识**：✅ **简单优先级，不做智能推断**

```
优先级：
1. provider.apiEndpoint（用户配置，最高优先级）
2. 平台默认端点
   - claude: /v1/messages
   - codex: /responses
```

---

### 争论点 4：配置验证

**我的担心**：用户填错端点

**codex 方案**：
- 前端：轻量校验（必须以 `/` 开头）
- 后端：不强校验，记录日志
- 测试：可选，不强制

**最终共识**：✅ **轻量校验 + 日志记录**

---

## 🛠️ 完整实施方案

### 后端修改

#### 1. services/providerservice.go

**添加字段**（第 26 行附近）：
```go
type Provider struct {
    // ... 现有字段 ...
    APIEndpoint string `json:"apiEndpoint,omitempty"` // 可选：覆盖平台默认端点
}
```

**添加方法**（GetEffectiveModel 附近）：
```go
// GetEffectiveEndpoint 获取有效的 API 端点
// 优先使用用户配置的端点，否则使用平台默认
func (p *Provider) GetEffectiveEndpoint(defaultEndpoint string) string {
    ep := strings.TrimSpace(p.APIEndpoint)
    if ep == "" {
        return defaultEndpoint
    }
    // 确保以 / 开头
    if !strings.HasPrefix(ep, "/") {
        ep = "/" + ep
    }
    return ep
}
```

**复制供应商时保留**（第 389 行附近）：
```go
cloned := &Provider{
    // ... 现有字段 ...
    APIEndpoint: source.APIEndpoint,
}
```

#### 2. services/providerrelay.go

**更新 4 处转发调用**：

**位置 1**：claude/codex 拉黑模式（约第 332 行）
```go
effectiveEndpoint := firstProvider.GetEffectiveEndpoint(endpoint)
ok, err := prs.forwardRequest(c, kind, *firstProvider, 1, effectiveEndpoint, query, ...)
```

**位置 2**：claude/codex 降级模式（约第 417 行）
```go
effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)
ok, err := prs.forwardRequest(c, kind, provider, 1, effectiveEndpoint, query, ...)
```

**位置 3**：custom CLI 拉黑模式（约第 1426 行）
```go
effectiveEndpoint := firstProvider.GetEffectiveEndpoint(endpoint)
ok, err := prs.forwardRequest(c, kind, *firstProvider, 1, effectiveEndpoint, query, ...)
```

**位置 4**：custom CLI 降级模式（约第 1492 行）
```go
effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)
ok, err := prs.forwardRequest(c, kind, provider, 1, effectiveEndpoint, query, ...)
```

---

### 前端修改

#### 1. frontend/src/data/cards.ts

**添加字段**：
```typescript
export type AutomationCard = {
  // ... 现有字段 ...
  apiEndpoint?: string  // 可选：API 端点路径
}
```

#### 2. frontend/src/components/Main/Index.vue

**VendorForm 添加字段**：
```typescript
type VendorForm = {
  // ... 现有字段 ...
  apiEndpoint?: string
}
```

**defaultFormValues 添加默认值**：
```typescript
const defaultFormValues = (platform?: string): VendorForm => ({
  // ... 现有字段 ...
  apiEndpoint: '',
})
```

**表单模板添加控件**（API URL 下方）：
```vue
<div class="form-field">
  <span>{{ t('components.main.form.labels.apiEndpoint') }}</span>
  <div class="endpoint-select-combo">
    <select v-model="modalState.form.apiEndpoint" class="endpoint-select">
      <option value="">{{ t('components.main.form.placeholders.defaultEndpoint') }}</option>
      <option value="/v1/messages">/v1/messages (Anthropic)</option>
      <option value="/v1/chat/completions">/v1/chat/completions (OpenAI/GLM 代理)</option>
      <option value="/api/paas/v4/chat/completions">/api/paas/v4/chat/completions (GLM 官方)</option>
      <option value="custom">{{ t('components.main.form.placeholders.customEndpoint') }}</option>
    </select>
    <BaseInput
      v-if="modalState.form.apiEndpoint === 'custom'"
      v-model="modalState.form.apiEndpoint"
      :placeholder="t('components.main.form.placeholders.enterCustomEndpoint')"
      class="endpoint-input"
    />
  </div>
  <span class="field-hint">
    {{ t('components.main.form.hints.apiEndpoint') }}
  </span>
</div>
```

#### 3. 国际化文本

**zh.json**：
```json
{
  "components": {
    "main": {
      "form": {
        "labels": {
          "apiEndpoint": "API 端点（可选）"
        },
        "placeholders": {
          "defaultEndpoint": "使用平台默认端点",
          "customEndpoint": "自定义端点...",
          "enterCustomEndpoint": "输入端点路径，如 /v1/chat/completions"
        },
        "hints": {
          "apiEndpoint": "留空使用平台默认（claude: /v1/messages, codex: /responses）。GLM 模型请使用 /v1/chat/completions"
        }
      }
    }
  }
}
```

**en.json**：
```json
{
  "components": {
    "main": {
      "form": {
        "labels": {
          "apiEndpoint": "API Endpoint (optional)"
        },
        "placeholders": {
          "defaultEndpoint": "Use platform default endpoint",
          "customEndpoint": "Custom endpoint...",
          "enterCustomEndpoint": "Enter endpoint path, e.g. /v1/chat/completions"
        },
        "hints": {
          "apiEndpoint": "Leave blank to use platform default (claude: /v1/messages, codex: /responses). For GLM models use /v1/chat/completions"
        }
      }
    }
  }
}
```

---

## 📊 改动范围

| 组件 | 文件数 | 代码行数 | 复杂度 |
|------|--------|---------|--------|
| 后端 | 2 | ~30 行 | 低 |
| 前端 | 3 | ~50 行 | 低 |
| 国际化 | 2 | ~20 行 | 低 |
| **总计** | **7** | **~100 行** | **低** |

---

## 🧪 测试场景

### 测试 1：GLM 供应商配置
1. 编辑 GLM 供应商
2. API 端点选择：`/v1/chat/completions`
3. 保存
4. 发送请求
5. **预期**：使用 /v1/chat/completions，不再 404

### 测试 2：默认行为
1. 编辑 Anthropic 供应商
2. API 端点：留空
3. 保存
4. **预期**：使用默认 /v1/messages

### 测试 3：自定义端点
1. 输入自定义端点
2. 校验：必须以 `/` 开头
3. **预期**：格式错误时提示

---

## ✅ 方案优势

| 特性 | 优势 |
|------|------|
| **改动最小** | 仅 7 个文件，~100 行代码 |
| **向后兼容** | 留空时行为完全不变 |
| **易于使用** | 下拉选择，不需要记忆端点 |
| **灵活性强** | 支持自定义端点 |
| **安全性好** | 轻量校验，记录日志 |

---

## ❓ 请审核确认

1. **是否同意这个方案？**
2. **UI 设计是否符合预期？**
3. **是否需要调整？**

**确认后，我将立即实施所有修改！**
