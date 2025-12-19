# 可用性监控开关关联方案（v2.1.1）

经过与 codex 的深入讨论，我们达成以下方案共识：

---

## 📋 需求确认

### 需求 1：双向开关同步
- 供应商编辑页面修改"可用性监控"开关 → 可用性页面自动同步
- 可用性页面修改开关 → 供应商列表自动同步
- **实时响应，无需手动刷新**

### 需求 2：应用设置控制自动轮询
- 应用设置中的开关 → 控制可用性监控是否每分钟自动检测
- 关闭 → 停止后台轮询
- 开启 → 启动每分钟检测
- **运行时立即生效，无需重启应用**

---

## 🎯 方案设计（与 codex 达成共识）

### 1. 双向开关同步方案

#### 采用方案：事件通知 + 即时刷新 + 路由兜底

**为什么选这个方案？**（与 codex 讨论后）
- ✅ **零新依赖**：不需要引入 Vuex/Pinia
- ✅ **简单可靠**：使用浏览器原生 CustomEvent
- ✅ **即时响应**：组件挂载时立即接收事件
- ✅ **有兜底机制**：路由切换时自动刷新数据

**实现细节**：

#### 前端 - 可用性页面

**文件**：`frontend/src/components/Availability/Index.vue`

**修改**：`toggleMonitor` 方法
```typescript
async function toggleMonitor(platform: string, providerId: number, enabled: boolean) {
  try {
    await setAvailabilityMonitorEnabled(platform, providerId, enabled)
    await loadData() // 立即刷新自己

    // 通知主列表页面刷新
    window.dispatchEvent(new CustomEvent('providers-updated', {
      detail: { platform, providerId, enabled }
    }))
  } catch (error) {
    console.error('Failed to toggle monitor:', error)
  }
}
```

#### 前端 - 主页面（供应商列表）

**文件**：`frontend/src/components/Main/Index.vue`

**新增**：事件监听
```typescript
// 在 onMounted 中添加
onMounted(async () => {
  await loadProvidersFromDisk()
  // ... 其他初始化代码 ...

  // 监听 providers 更新事件
  const handleProvidersUpdate = () => {
    loadProvidersFromDisk()
  }
  window.addEventListener('providers-updated', handleProvidersUpdate)

  // 清理监听器
  onUnmounted(() => {
    window.removeEventListener('providers-updated', handleProvidersUpdate)
  })
})
```

**修改**：`submitModal` 方法（保存成功后通知）
```typescript
const submitModal = async () => {
  // ... 现有保存逻辑 ...

  await persistProviders(modalState.tabId)
  closeModal()

  // 通知可用性页面刷新
  window.dispatchEvent(new CustomEvent('providers-updated'))
}
```

---

### 2. 应用设置关联方案

#### 采用方案：新增字段 + 向后兼容

**为什么这样设计？**（与 codex 讨论后）
- ✅ **清晰命名**：`AutoAvailabilityMonitor` 明确表达功能
- ✅ **向后兼容**：读取时 fallback 到旧字段
- ✅ **过渡平滑**：旧版本升级无需迁移

#### 后端 - AppSettings 结构

**文件**：`services/appsettings.go`

**新增字段**：
```go
type AppSettings struct {
    // ... 现有字段 ...
    AutoConnectivityTest bool `json:"auto_connectivity_test"` // 已废弃，保留兼容
    AutoAvailabilityMonitor bool `json:"auto_availability_monitor"` // 新字段
}
```

**读取时的兼容逻辑**：
```go
func (as *AppSettingsService) GetAppSettings() (*AppSettings, error) {
    settings := loadFromFile()

    // 向后兼容：如果新字段未设置，使用旧字段值
    if !settings.AutoAvailabilityMonitor && settings.AutoConnectivityTest {
        settings.AutoAvailabilityMonitor = settings.AutoConnectivityTest
    }

    return settings, nil
}
```

#### 后端 - HealthCheckService 轮询控制

**文件**：`services/healthcheckservice.go`

**新增字段和方法**：
```go
type HealthCheckService struct {
    // ... 现有字段 ...
    autoPollingEnabled bool
    mu sync.RWMutex
}

// SetAutoAvailabilityPolling 设置是否自动轮询（立即生效）
func (hcs *HealthCheckService) SetAutoAvailabilityPolling(enabled bool) {
    hcs.mu.Lock()
    defer hcs.mu.Unlock()

    hcs.autoPollingEnabled = enabled

    if enabled && !hcs.running {
        // 启动轮询
        hcs.StartBackgroundPolling()
    } else if !enabled && hcs.running {
        // 停止轮询
        hcs.StopBackgroundPolling()
    }
}

// IsAutoAvailabilityPollingEnabled 查询自动轮询状态
func (hcs *HealthCheckService) IsAutoAvailabilityPollingEnabled() bool {
    hcs.mu.RLock()
    defer hcs.mu.RUnlock()
    return hcs.autoPollingEnabled
}
```

#### 后端 - main.go 启动逻辑

**文件**：`main.go`

**修改启动流程**：
```go
// 当前（行 188-191）：
go func() {
    time.Sleep(5 * time.Second)
    healthCheckService.StartBackgroundPolling()
    log.Println("✅ 可用性健康监控已启动")
}()

// 改为：
go func() {
    time.Sleep(5 * time.Second)
    settings, err := appSettings.GetAppSettings()
    if err != nil {
        log.Printf("读取应用设置失败: %v", err)
        return
    }

    // 使用新字段，兼容旧字段
    autoEnabled := settings.AutoAvailabilityMonitor || settings.AutoConnectivityTest
    if autoEnabled {
        healthCheckService.SetAutoAvailabilityPolling(true)
        log.Println("✅ 可用性自动监控已启动")
    } else {
        log.Println("ℹ️ 可用性自动监控已禁用（可在设置中开启）")
    }
}()
```

#### 前端 - 设置页面

**文件**：`frontend/src/components/General/Index.vue`

**修改**：
1. 改名："自动连通性检测" → "自动可用性监控"
2. 调用新的后端方法

```vue
<!-- 开关绑定 -->
<input v-model="autoAvailabilityMonitor" type="checkbox" />

<!-- 保存方法 -->
async function saveSettings() {
  const settings = {
    // ... 其他设置 ...
    auto_availability_monitor: autoAvailabilityMonitor.value,
    auto_connectivity_test: autoAvailabilityMonitor.value, // 过渡期同步
  }

  await saveAppSettings(settings)

  // 立即生效
  await Call.ByName(
    'codeswitch/services.HealthCheckService.SetAutoAvailabilityPolling',
    autoAvailabilityMonitor.value
  )

  showToast('设置已保存', 'success')
}
```

---

## 📊 数据流图（文字描述）

### 流程 1：供应商编辑 → 可用性页面同步

```
用户在主页面编辑 Provider
  ↓
修改 availabilityMonitorEnabled 字段
  ↓
submitModal() 保存
  ↓
SaveProviders() 写入 ~/.code-switch/*.json
  ↓
dispatch('providers-updated') 事件
  ↓
可用性页面监听到事件
  ↓
loadData() 重新加载
  ↓
UI 自动更新，开关状态同步 ✓
```

### 流程 2：可用性页面 → 供应商列表同步

```
用户在可用性页面切换开关
  ↓
toggleMonitor() 调用
  ↓
setAvailabilityMonitorEnabled() 后端保存
  ↓
loadData() 刷新自己
  ↓
dispatch('providers-updated') 事件
  ↓
主页面监听到事件
  ↓
loadProvidersFromDisk() 重新加载
  ↓
卡片状态自动更新 ✓
```

### 流程 3：应用设置控制自动轮询

```
用户在设置页面切换开关
  ↓
saveAppSettings() 保存配置
  ↓
SetAutoAvailabilityPolling(enabled) 立即启停
  ↓
如果 enabled = true:
  StartBackgroundPolling() → 每60秒检测一次
如果 enabled = false:
  StopBackgroundPolling() → 停止检测
  ↓
可用性页面显示轮询状态更新 ✓
```

---

## 📝 实施清单

### 后端修改（3个文件）

1. **services/appsettings.go**
   - [ ] 添加 `AutoAvailabilityMonitor` 字段
   - [ ] 添加向后兼容读取逻辑

2. **services/healthcheckservice.go**
   - [ ] 添加 `autoPollingEnabled` 字段
   - [ ] 添加 `SetAutoAvailabilityPolling` 方法
   - [ ] 添加 `IsAutoAvailabilityPollingEnabled` 方法

3. **main.go**
   - [ ] 修改启动逻辑，读取设置决定是否启动轮询

### 前端修改（3个文件）

1. **frontend/src/components/Availability/Index.vue**
   - [ ] 修改 `toggleMonitor` 方法，添加事件通知

2. **frontend/src/components/Main/Index.vue**
   - [ ] 添加事件监听器
   - [ ] 修改 `submitModal`，添加事件通知

3. **frontend/src/components/General/Index.vue**
   - [ ] 改名："自动连通性检测" → "自动可用性监控"
   - [ ] 调用新的后端方法

### 国际化修改（2个文件）

1. **frontend/src/locales/zh.json**
   - [ ] 更新设置页面的文案

2. **frontend/src/locales/en.json**
   - [ ] 更新设置页面的文案

---

## ⚠️ 注意事项

### 1. 事件可靠性
- CustomEvent 只在同一窗口内有效
- 如果页面未挂载，依赖路由进入时的自动刷新
- 不会丢失数据（数据已写入文件）

### 2. 向后兼容
- 旧配置文件自动兼容
- 读取时优先新字段，fallback 旧字段
- 过渡期同时写入新旧字段

### 3. 性能影响
- 事件通知：零性能开销
- 自动刷新：仅在修改时触发
- 后台轮询：可动态启停，节省资源

---

## 🧪 测试场景

### 测试 1：双向同步
1. 在主页面启用某个 Provider 的可用性监控
2. 切换到可用性页面
3. **预期**：对应的开关自动显示为"已启用"
4. 在可用性页面关闭该 Provider
5. 切换回主页面
6. **预期**：对应的状态自动更新

### 测试 2：应用设置控制
1. 在设置页面关闭"自动可用性监控"
2. **预期**：后台轮询立即停止
3. 可用性页面不再每60秒刷新
4. 在设置页面开启"自动可用性监控"
5. **预期**：后台轮询立即启动
6. 可用性页面开始每60秒自动检测

---

## ✅ 方案优势

| 特性 | 方案优势 |
|------|---------|
| **实时性** | 事件通知，立即同步 |
| **可靠性** | 文件持久化 + 路由兜底 |
| **简洁性** | 零新依赖，最小改动 |
| **兼容性** | 向后兼容旧配置 |
| **可控性** | 运行时动态启停 |

---

## 📊 预估工作量

| 组件 | 文件数 | 代码行数 | 复杂度 |
|------|--------|---------|--------|
| 后端 | 3 | ~60 行 | 中 |
| 前端 | 3 | ~40 行 | 低 |
| 国际化 | 2 | ~10 行 | 低 |
| **总计** | **8** | **~110 行** | **中** |

---

## ❓ 请审核确认

### 需求确认
1. ✅ 双向开关同步（供应商编辑 ↔ 可用性页面）
2. ✅ 应用设置控制自动轮询

### 方案确认
1. ✅ 使用 CustomEvent 实现同步（codex 推荐）
2. ✅ 新增 `AutoAvailabilityMonitor` 字段兼容旧字段
3. ✅ 运行时动态启停后台轮询

### 实施确认
1. **是否同意这个方案？**
2. **是否需要调整？**
3. **是否可以开始实施？**

---

**请审核方案，确认后我将立即实施！**
