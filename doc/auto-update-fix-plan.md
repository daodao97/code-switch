# 自动更新系统修复方案 v1.0

> 基于 Codex 多轮讨论确认的最终修复方案
> 创建时间: 2025-12-13
> 预估失败率: 修复前 8-20% → 修复后 <2%

---

## 一、问题分级

### P0 - 致命问题（必须立即修复）

| ID | 问题 | 影响 | 文件位置 |
|----|------|------|----------|
| P0-1 | Windows 启动恢复 nil panic | 崩溃 | `main.go:437-448` |
| P0-2 | 锁文件递归死循环 | 卡死 | `updateservice.go:1740-1741` |
| P0-3 | version/SHA 竞态条件 | 校验失败 | `updateservice.go:222-226, 538-546` |

### P1 - 严重问题（应尽快修复）

| ID | 问题 | 影响 | 文件位置 |
|----|------|------|----------|
| P1-1 | SaveState 非原子写入 | 状态损坏 | `updateservice.go:1604-1614` |
| P1-2 | LoadState 错误被吞没 | 静默失败 | `main.go:103` |
| P1-3 | dailyCheckTimer 无锁访问 | 竞态 | `updateservice.go:1424, 1444-1447` |
| P1-4 | updater 无校验降级 | 安全风险 | `updateservice.go:1873-1882` |
| P1-5 | pending 清理时机错误 | 更新失败 | 多处 |
| P1-6 | 锁心跳 + stale 判定优化 | 误删锁 | `updateservice.go:1729-1755` |
| P1-7 | macOS/Linux 缺失启动恢复 | 无法回滚 | `main.go:416-459` |

---

## 二、修复实施顺序

```
P0-1 → P0-2 → P0-3 → P1-1 → P1-2 → P1-3 → P1-4 → P1-5 → P1-6 → P1-7
```

依赖关系：
- P0-3 依赖于理解 PrepareUpdate 流程
- P1-5 依赖于 P0-3（pending 清理逻辑）
- P1-7 依赖于 P0-1 模式（启动恢复）

---

## 三、详细修复方案

### P0-1: 修复 Windows 启动恢复 nil panic

**问题**: `os.Stat(currentExe)` 失败时 `currentInfo` 为 nil，但后续代码仍访问 `currentInfo.Size()`

**位置**: `main.go:437-448`

**修复**:
```go
// 修复前
currentInfo, err := os.Stat(currentExe)
currentOK := err == nil && currentInfo.Size() > 1024*1024

// 修复后
currentInfo, err := os.Stat(currentExe)
if err != nil {
    // 当前 exe 不存在或无法访问，需要回滚
    log.Printf("[Recovery] 当前版本不可访问: %v，从备份恢复", err)
    if err := os.Rename(backupPath, currentExe); err != nil {
        log.Printf("[Recovery] 回滚失败: %v", err)
        log.Println("[Recovery] 请手动将备份文件恢复为原文件名")
    } else {
        log.Println("[Recovery] 回滚成功，已恢复到旧版本")
    }
    return
}
currentOK := currentInfo.Size() > 1024*1024
```

---

### P0-2: 修复锁文件递归问题

**问题**: `acquireUpdateLock()` 递归调用自身，无次数限制可能死循环

**位置**: `updateservice.go:1729-1755`

**修复**:
```go
func (us *UpdateService) acquireUpdateLock() error {
    lockPath := filepath.Join(us.updateDir, "update.lock")

    for attempt := 0; attempt < 2; attempt++ {
        f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
        if err == nil {
            // 成功获取锁
            fmt.Fprintf(f, "%d\n%s", os.Getpid(), time.Now().Format(time.RFC3339))
            f.Close()
            us.lockFile = lockPath
            log.Printf("[UpdateService] 已获取更新锁: %s", lockPath)
            return nil
        }

        if !os.IsExist(err) {
            return fmt.Errorf("创建锁文件失败: %w", err)
        }

        // 锁文件已存在，检查是否过期
        info, statErr := os.Stat(lockPath)
        if statErr != nil {
            // stat 失败，可能锁被删除，重试
            continue
        }

        if time.Since(info.ModTime()) > 10*time.Minute {
            log.Printf("[UpdateService] 检测到过期锁文件（mtime=%v），强制删除: %s",
                info.ModTime(), lockPath)
            if rmErr := os.Remove(lockPath); rmErr != nil {
                return fmt.Errorf("删除过期锁失败: %w", rmErr)
            }
            continue // 重试获取
        }

        return fmt.Errorf("另一个更新正在进行中")
    }

    return fmt.Errorf("获取锁失败：重试次数耗尽")
}
```

---

### P0-3: 从 pending 恢复 version + 版本/SHA 竞态

**问题**:
1. `CheckUpdate()` 更新共享字段 `us.latestVersion`，但 `PrepareUpdate()` 在不同时机读取
2. `DownloadUpdate()` 开始时快照 SHA，但未快照 version

**位置**:
- `updateservice.go:222-226` (CheckUpdate 写入)
- `updateservice.go:354-359` (DownloadUpdate 读取)
- `updateservice.go:538-546` (PrepareUpdate 写入 pending)

**修复策略**:
1. `DownloadUpdate()` 开始时同时快照 version 和 SHA
2. 将快照值传递给 `PrepareUpdate()`
3. `ApplyUpdate()` 从 pending 文件恢复 version（用于日志）

**修复** - 添加内部方法:
```go
// prepareUpdateInternal 内部方法，接收明确的 version 和 sha256
func (us *UpdateService) prepareUpdateInternal(version, sha256 string) error {
    log.Printf("[UpdateService] 准备更新...")

    us.mu.Lock()
    if us.updateFilePath == "" {
        us.mu.Unlock()
        return fmt.Errorf("更新文件路径为空")
    }
    filePath := us.updateFilePath
    us.mu.Unlock()

    // 写入待更新标记
    pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
    metadata := map[string]interface{}{
        "version":       version,
        "download_path": filePath,
        "download_time": time.Now().Format(time.RFC3339),
    }
    if sha256 != "" {
        metadata["sha256"] = sha256
    }

    data, err := json.MarshalIndent(metadata, "", "  ")
    if err != nil {
        return fmt.Errorf("序列化元数据失败: %w", err)
    }

    if err := os.WriteFile(pendingFile, data, 0o644); err != nil {
        return fmt.Errorf("写入标记文件失败: %w", err)
    }

    us.mu.Lock()
    us.updateReady = true
    us.mu.Unlock()

    us.SaveState()
    log.Printf("[UpdateService] ✅ 更新已准备就绪")
    return nil
}

// PrepareUpdate 公开方法（已废弃，仅保留兼容性）
// Deprecated: 请使用 DownloadUpdate 自动调用内部 prepare
func (us *UpdateService) PrepareUpdate() error {
    us.mu.Lock()
    version := us.latestVersion
    sha := ""
    if us.latestUpdateInfo != nil {
        sha = us.latestUpdateInfo.SHA256
    }
    us.mu.Unlock()
    return us.prepareUpdateInternal(version, sha)
}
```

**修复** - DownloadUpdate 快照:
```go
func (us *UpdateService) DownloadUpdate(progressCallback func(float64)) error {
    // ... 获取锁 ...

    us.mu.Lock()
    url := us.downloadURL
    // 快照 version 和 SHA256
    snapshotVersion := us.latestVersion
    snapshotSHA := ""
    if us.latestUpdateInfo != nil {
        snapshotSHA = us.latestUpdateInfo.SHA256
    }
    us.mu.Unlock()

    // ... 下载逻辑 ...

    // 使用快照值调用内部方法
    if err := us.prepareUpdateInternal(snapshotVersion, snapshotSHA); err != nil {
        return fmt.Errorf("准备更新失败: %w", err)
    }

    return nil
}
```

---

### P1-1: 原子写状态文件

**问题**: `SaveState()` 直接写入目标文件，断电可能损坏

**位置**: `updateservice.go:1604-1614`

**修复**: 创建跨平台原子写入

**新文件** `services/atomic_write.go`:
```go
package services

// atomicWriteFile 原子写入文件（跨平台）
// 策略：写入临时文件 → fsync → 重命名
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
    dir := filepath.Dir(path)
    tmp, err := os.CreateTemp(dir, ".tmp-*")
    if err != nil {
        return fmt.Errorf("创建临时文件失败: %w", err)
    }
    tmpPath := tmp.Name()

    // 写入数据
    if _, err := tmp.Write(data); err != nil {
        tmp.Close()
        os.Remove(tmpPath)
        return fmt.Errorf("写入数据失败: %w", err)
    }

    // 确保数据落盘
    if err := tmp.Sync(); err != nil {
        tmp.Close()
        os.Remove(tmpPath)
        return fmt.Errorf("sync 失败: %w", err)
    }
    tmp.Close()

    // 原子重命名
    return atomicRename(tmpPath, path)
}
```

**新文件** `services/atomic_write_windows.go`:
```go
//go:build windows

package services

import (
    "os"
    "syscall"
    "time"
    "unsafe"
)

var (
    kernel32         = syscall.NewLazyDLL("kernel32.dll")
    procMoveFileExW  = kernel32.NewProc("MoveFileExW")
)

const MOVEFILE_REPLACE_EXISTING = 0x1

func atomicRename(src, dst string) error {
    srcPtr, _ := syscall.UTF16PtrFromString(src)
    dstPtr, _ := syscall.UTF16PtrFromString(dst)

    for attempt := 0; attempt < 3; attempt++ {
        ret, _, err := procMoveFileExW.Call(
            uintptr(unsafe.Pointer(srcPtr)),
            uintptr(unsafe.Pointer(dstPtr)),
            MOVEFILE_REPLACE_EXISTING,
        )
        if ret != 0 {
            return nil // 成功
        }

        // ERROR_SHARING_VIOLATION = 32
        if err == syscall.Errno(32) && attempt < 2 {
            time.Sleep(100 * time.Millisecond)
            continue
        }

        os.Remove(src)
        return fmt.Errorf("MoveFileEx 失败: %w", err)
    }

    os.Remove(src)
    return fmt.Errorf("MoveFileEx 重试耗尽")
}
```

**新文件** `services/atomic_write_nonwindows.go`:
```go
//go:build !windows

package services

import "os"

func atomicRename(src, dst string) error {
    if err := os.Rename(src, dst); err != nil {
        os.Remove(src)
        return err
    }
    return nil
}
```

**修改** `SaveState()`:
```go
func (us *UpdateService) SaveState() error {
    // ... 构建 state ...

    data, err := json.MarshalIndent(state, "", "  ")
    if err != nil {
        return fmt.Errorf("序列化状态失败: %w", err)
    }

    dir := filepath.Dir(us.stateFile)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return fmt.Errorf("创建目录失败: %w", err)
    }

    return atomicWriteFile(us.stateFile, data, 0o644)
}
```

---

### P1-2: 不吞 LoadState 错误

**问题**: `main.go` 中 `_ = us.LoadState()` 吞掉错误

**位置**: `services/updateservice.go:103` 被调用处

**修复**: 记录错误但继续运行（状态文件损坏不应阻止启动）

```go
// NewUpdateService 中
if err := us.LoadState(); err != nil {
    log.Printf("[UpdateService] ⚠️ 加载状态失败（将使用默认值）: %v", err)
}
```

---

### P1-3: dailyCheckTimer 加锁

**问题**: `dailyCheckTimer` 字段在多个 goroutine 中访问但无锁保护

**位置**: `updateservice.go:1424, 1444-1447`

**修复**:
```go
func (us *UpdateService) StartDailyCheck() {
    us.mu.Lock()
    if us.dailyCheckTimer != nil {
        us.dailyCheckTimer.Stop()
        us.dailyCheckTimer = nil
    }

    if !us.autoCheckEnabled {
        us.mu.Unlock()
        log.Println("[UpdateService] 自动检查已禁用，不启动定时器")
        return
    }
    us.mu.Unlock()

    duration := us.calculateNextCheckDuration()

    us.mu.Lock()
    us.dailyCheckTimer = time.AfterFunc(duration, func() {
        // ... 回调逻辑 ...
    })
    us.mu.Unlock()

    log.Printf("[UpdateService] 定时检查已启动，下次: %s",
        time.Now().Add(duration).Format("2006-01-02 15:04:05"))
}

func (us *UpdateService) StopDailyCheck() {
    us.mu.Lock()
    defer us.mu.Unlock()

    if us.dailyCheckTimer != nil {
        us.dailyCheckTimer.Stop()
        us.dailyCheckTimer = nil
    }
}
```

---

### P1-4: 禁止 updater 无校验降级

**问题**: `downloadUpdater()` 在校验失败时降级为无校验下载

**位置**: `updateservice.go:1869-1897`

**修复**: 移除降级逻辑，校验失败即失败

```go
func (us *UpdateService) downloadUpdater(targetPath string) error {
    updaterPath, err := us.downloadAndVerify("updater.exe")
    if err != nil {
        // 不再降级，直接返回错误
        return fmt.Errorf("下载 updater.exe 失败（需要 SHA256 校验）: %w", err)
    }

    if updaterPath != targetPath {
        if err := os.Rename(updaterPath, targetPath); err != nil {
            if err := copyUpdateFile(updaterPath, targetPath); err != nil {
                return fmt.Errorf("移动 updater.exe 失败: %w", err)
            }
            os.Remove(updaterPath)
        }
    }

    return nil
}
```

---

### P1-5: pending 清理延后 + 平台联动

**问题**: 各平台在不同时机清理 pending，可能导致更新失败后无法重试

**核心原则**:
1. 脚本/updater 只在 **SUCCESS** 时删除 pending
2. 脚本/updater **总是** 删除 lock（无论成功失败）
3. 主程序启动恢复检测到 `.old` 存在时决定是否清理

**修复** - PowerShell 脚本 (applyPortableUpdate):
```powershell
# 在脚本末尾，成功后清理
$pendingFile = Join-Path (Split-Path $currentExe -Parent) '.code-switch\.pending-update'
if (Test-Path $pendingFile) {
    Remove-Item $pendingFile -Force -ErrorAction SilentlyContinue
    Log "cleanup pending"
}

# 总是清理 lock
$lockFile = Join-Path (Split-Path $currentExe -Parent) '.code-switch\updates\update.lock'
if (Test-Path $lockFile) {
    Remove-Item $lockFile -Force -ErrorAction SilentlyContinue
    Log "cleanup lock"
}
```

**修复** - Bash 脚本 (applyUpdateDarwin/applyUpdateLinux):
```bash
# 成功后清理 pending
PENDING_FILE="$HOME/.code-switch/.pending-update"
if [ -f "$PENDING_FILE" ]; then
    rm -f "$PENDING_FILE" 2>/dev/null || true
    log "cleanup pending"
fi

# 总是清理 lock (使用 trap)
LOCK_FILE="$HOME/.code-switch/updates/update.lock"
cleanup_lock() {
    rm -f "$LOCK_FILE" 2>/dev/null || true
    log "cleanup lock"
}
trap cleanup_lock EXIT
```

**修复** - updater.exe:
```go
// main() 末尾
defer func() {
    // 总是清理 lock
    lockFile := filepath.Join(filepath.Dir(taskFile), "update.lock")
    os.Remove(lockFile)
    log.Printf("已清理锁文件: %s", lockFile)
}()

// 只在成功时清理 pending
if updateSuccess {
    pendingFile := filepath.Join(os.Getenv("USERPROFILE"), ".code-switch", ".pending-update")
    os.Remove(pendingFile)
    log.Printf("已清理 pending: %s", pendingFile)
}
```

**修复** - 主程序 clearPendingState() 调用时机:
移除 `applyPortableUpdate`、`applyInstalledUpdate`、`applyUpdateDarwin`、`applyUpdateLinux` 中对 `clearPendingState()` 的调用，改由脚本/updater 负责。

---

### P1-6: 锁心跳 + stale 判定优化

**问题**: 仅靠 mtime 判断锁是否过期不够可靠

**修复**: 添加心跳机制（可选增强）

```go
// 在 DownloadUpdate 中启动心跳
func (us *UpdateService) startLockHeartbeat(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                if us.lockFile != "" {
                    // 更新 mtime
                    now := time.Now()
                    os.Chtimes(us.lockFile, now, now)
                }
            }
        }
    }()
}
```

---

### P1-7: macOS/Linux 启动恢复

**问题**: 只有 Windows 有启动恢复逻辑

**修复**: 在 `main.go` 中添加跨平台恢复

```go
func checkAndRecoverFromFailedUpdate() {
    var currentExe string
    var err error

    switch runtime.GOOS {
    case "windows":
        currentExe, err = os.Executable()
        if err != nil {
            return
        }
        currentExe, _ = filepath.EvalSymlinks(currentExe)

    case "darwin":
        // macOS: 检查 .app 包
        currentExe, err = os.Executable()
        if err != nil {
            return
        }
        currentExe, _ = filepath.EvalSymlinks(currentExe)
        // 找到 .app 根目录
        appPath := currentExe
        for i := 0; i < 6; i++ {
            if strings.HasSuffix(strings.ToLower(appPath), ".app") {
                break
            }
            appPath = filepath.Dir(appPath)
        }
        if strings.HasSuffix(strings.ToLower(appPath), ".app") {
            currentExe = appPath
        }

    case "linux":
        // Linux: 检查 APPIMAGE 环境变量
        if appimage := os.Getenv("APPIMAGE"); appimage != "" {
            currentExe = appimage
        } else {
            currentExe, err = os.Executable()
            if err != nil {
                return
            }
            currentExe, _ = filepath.EvalSymlinks(currentExe)
        }

    default:
        return
    }

    backupPath := currentExe + ".old"

    // 检查备份文件是否存在
    backupInfo, err := os.Stat(backupPath)
    if err != nil {
        return // 无备份，正常情况
    }

    log.Printf("[Recovery] 检测到备份: %s (size=%d)", backupPath, backupInfo.Size())

    // 检查当前版本是否可用
    currentInfo, err := os.Stat(currentExe)
    if err != nil {
        log.Printf("[Recovery] 当前版本不可访问: %v，从备份恢复", err)
        doRecovery(backupPath, currentExe)
        return
    }

    // 根据平台判断最小有效大小
    minSize := int64(1024 * 1024) // 默认 1MB
    if runtime.GOOS == "darwin" {
        minSize = 10 * 1024 * 1024 // macOS .app 至少 10MB
    }

    if currentInfo.Size() > minSize {
        // 当前版本正常，清理备份
        log.Println("[Recovery] 更新成功，清理旧版本备份")
        if err := os.RemoveAll(backupPath); err != nil {
            log.Printf("[Recovery] 删除备份失败: %v", err)
        }
    } else {
        log.Printf("[Recovery] 当前版本异常（size=%d < %d），从备份恢复",
            currentInfo.Size(), minSize)
        doRecovery(backupPath, currentExe)
    }
}

func doRecovery(backupPath, targetPath string) {
    // 删除损坏的当前版本
    if err := os.RemoveAll(targetPath); err != nil {
        log.Printf("[Recovery] 删除损坏文件失败: %v", err)
    }

    // 恢复备份
    if err := os.Rename(backupPath, targetPath); err != nil {
        log.Printf("[Recovery] 回滚失败: %v", err)
        log.Println("[Recovery] 请手动将备份恢复")
    } else {
        log.Println("[Recovery] 回滚成功")
    }
}
```

---

## 四、测试检查清单

### 功能测试

- [ ] Windows 便携版更新（正常流程）
- [ ] Windows 便携版更新（中途断电恢复）
- [ ] Windows 安装版更新（UAC 确认）
- [ ] Windows 安装版更新（UAC 取消）
- [ ] macOS 更新（正常流程）
- [ ] macOS 更新（中途断电恢复）
- [ ] Linux AppImage 更新（正常流程）
- [ ] Linux AppImage 更新（中途断电恢复）

### 边界测试

- [ ] 并发启动两个实例时的锁竞争
- [ ] 下载过程中网络断开
- [ ] SHA256 校验失败
- [ ] 磁盘空间不足
- [ ] 目标目录无写权限

### 回归测试

- [ ] 定时检查功能正常
- [ ] 手动检查更新正常
- [ ] 状态持久化正常
- [ ] 前端进度显示正常

---

## 五、实施进度

| ID | 描述 | 状态 | 完成时间 |
|----|------|------|----------|
| P0-1 | Windows 启动恢复 panic | ⏳ 待实施 | - |
| P0-2 | 锁文件递归问题 | ⏳ 待实施 | - |
| P0-3 | version/SHA 竞态 | ⏳ 待实施 | - |
| P1-1 | 原子写状态文件 | ⏳ 待实施 | - |
| P1-2 | LoadState 错误处理 | ⏳ 待实施 | - |
| P1-3 | dailyCheckTimer 加锁 | ⏳ 待实施 | - |
| P1-4 | updater 无校验降级 | ⏳ 待实施 | - |
| P1-5 | pending 清理延后 | ⏳ 待实施 | - |
| P1-6 | 锁心跳优化 | ⏳ 待实施 | - |
| P1-7 | macOS/Linux 启动恢复 | ⏳ 待实施 | - |

---

## 六、审核检查点

修复完成后需要 Codex 审核：

1. **完整性审核**: 所有 P0/P1 问题是否都已修复
2. **回归审核**: 修复是否引入新问题
3. **边界情况审核**: 边界条件处理是否完善
4. **代码质量审核**: 代码风格、错误处理是否规范
