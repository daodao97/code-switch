# Windows 安装版无界面静默更新方案

> **文档状态**：设计方案（待实现）
> **创建日期**：2025-12-01
> **当前状态**：设计完成，代码未实现

---

## 零、实现差距核查（重要）

以下是设计方案与现有代码的差距，实现时需逐一解决：

### 必须实现的内容

| 序号 | 差距项 | 现状 | 需要做的事 |
|------|--------|------|-----------|
| 1 | updater.exe 不存在 | 无此文件 | 新建 `cmd/updater/main.go` 和 manifest |
| 2 | 下载目标错误 | 安装版下载 `installer.exe` | 修改 `findPlatformAsset` 改为下载 `CodeSwitch.exe` |
| 3 | 更新逻辑未实现 | 使用 PowerShell 启动安装器 | 新增 `applyInstalledUpdate` 方法 |
| 4 | 构建任务缺失 | 无 `build:updater` | 修改 `Taskfile.yml` |
| 5 | 发布流程缺失 | 未发布 `updater.exe` | 修改 `publish_release.sh` |

### 风险点（实现时必须注意）

| 风险 | 说明 | 解决方案 |
|------|------|---------|
| **大小写不一致** | 发布脚本用 `bin/codeswitch.exe`（小写），`findPlatformAsset` 匹配 `CodeSwitch.exe`（大写），在大小写敏感环境会失败 | 统一使用 `CodeSwitch.exe`（大写），修改发布脚本 |
| **测试覆盖缺失** | 无更新流程的单元/集成测试 | 实现时同步编写测试用例 |
| **日志/回滚缺失** | 当前代码无等待/回滚/日志机制 | 按设计实现完整的日志和回滚 |
| **安全校验缺失** | 下载文件无 SHA256 校验或签名验证 | Release 提供哈希文件，下载后校验 |
| **回滚场景不全** | 未覆盖 updater 崩溃、备份占用、重启失败 | 见下方"完整回滚策略" |
| **路径判定简化** | 自定义安装路径（如 D:\Apps）可能误判 | 见下方"路径判定策略" |
| **超时固定值** | 30 秒可能不够（大文件/系统繁忙） | 参数化，写入任务文件 |
| **残留文件累积** | 旧安装器、旧 updater、旧备份未清理 | 见下方"清理策略" |
| **多用户会话** | 更新状态文件可能冲突 | 使用用户级目录隔离 |
| **前端交互不明确** | 弹窗内容、失败提示、重试入口未定义 | 见下方"前端交互规范" |

---

## 一、问题描述

### 当前痛点

| 问题 | 说明 |
|------|------|
| 下载量大 | 安装版更新需下载完整 NSIS 安装器（~15MB） |
| 体验差 | 弹出安装器界面，走完整安装流程 |
| 速度慢 | 完整安装流程耗时较长 |

### 用户期望

```
定时检查 → 后台下载 → 弹窗询问 → 点击确认 → 关闭重启 → 完成！
```

**核心诉求**：关闭重启就更新好了，不需要看到安装器界面。

## 二、方案概述

采用 **updater.exe 辅助进程** 方案：

1. 下载核心 `CodeSwitch.exe`（~10MB）而非完整安装器
2. 使用独立的 `updater.exe` 辅助程序完成文件替换
3. `updater.exe` 内嵌 UAC manifest，自动请求管理员权限
4. 用户体验：点击更新 → UAC 确认 → 几秒后新版本启动

### 方案对比

| 方面 | 当前实现 | 新方案 |
|------|---------|--------|
| 下载文件 | 安装器 ~15MB | 核心 exe ~10MB |
| 更新方式 | 启动 NSIS 安装器 | updater.exe 静默替换 |
| 用户交互 | 安装器界面 + UAC | 仅一次 UAC |
| 更新速度 | 慢（完整安装流程） | 快（秒级替换） |

## 三、技术约束

1. **Windows 文件锁定**：运行中的 .exe 无法被替换
2. **Program Files 权限**：需要管理员权限才能写入
3. **UAC 不可避免**：安装在 Program Files 必须提权

## 四、详细实现

### 4.1 新建 updater.exe 辅助程序

#### 文件：`cmd/updater/main.go`

```go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "log"
    "os"
    "os/exec"
    "path/filepath"
    "time"

    "golang.org/x/sys/windows"
)

// UpdateTask 更新任务配置
type UpdateTask struct {
    MainPID      int      `json:"main_pid"`       // 主程序 PID
    TargetExe    string   `json:"target_exe"`     // 目标可执行文件路径
    NewExePath   string   `json:"new_exe_path"`   // 新版本文件路径
    BackupPath   string   `json:"backup_path"`    // 备份路径
    CleanupPaths []string `json:"cleanup_paths"`  // 需要清理的临时文件
    TimeoutSec   int      `json:"timeout_sec"`    // 必填：等待超时（秒），由主程序动态计算
}

func main() {
    if len(os.Args) < 2 {
        log.Fatal("Usage: updater.exe <task-file>")
    }

    taskFile := os.Args[1]

    // 设置日志文件
    logPath := filepath.Join(filepath.Dir(taskFile), "update.log")
    logFile, _ := os.Create(logPath)
    if logFile != nil {
        defer logFile.Close()
        log.SetOutput(logFile)
    }

    // 读取任务配置
    data, err := os.ReadFile(taskFile)
    if err != nil {
        log.Fatalf("读取任务文件失败: %v", err)
    }

    var task UpdateTask
    if err := json.Unmarshal(data, &task); err != nil {
        log.Fatalf("解析任务配置失败: %v", err)
    }

    log.Printf("开始更新任务: PID=%d, Target=%s, Timeout=%ds", task.MainPID, task.TargetExe, task.TimeoutSec)

    // 等待主程序退出（使用任务配置的超时值，禁止硬编码）
    timeout := time.Duration(task.TimeoutSec) * time.Second
    if task.TimeoutSec <= 0 {
        timeout = 30 * time.Second // 兜底默认值（仅当任务文件异常时）
        log.Println("[警告] timeout_sec 未设置，使用默认 30 秒")
    }
    if err := waitForProcessExit(task.MainPID, timeout); err != nil {
        log.Fatalf("等待主程序退出超时（%ds）: %v", task.TimeoutSec, err)
    }
    log.Println("主程序已退出")

    // 执行更新
    if err := performUpdate(task); err != nil {
        log.Printf("更新失败: %v，执行回滚", err)
        rollback(task)
        os.Exit(1)
    }

    log.Println("更新成功，启动新版本...")

    // 启动新版本
    cmd := exec.Command(task.TargetExe)
    cmd.Dir = filepath.Dir(task.TargetExe)
    if err := cmd.Start(); err != nil {
        log.Printf("启动新版本失败: %v", err)
    }

    // 延迟清理临时文件
    time.Sleep(3 * time.Second)
    for _, path := range task.CleanupPaths {
        os.RemoveAll(path)
        log.Printf("已清理: %s", path)
    }
    os.Remove(taskFile)
}

// waitForProcessExit 等待指定 PID 的进程退出
func waitForProcessExit(pid int, timeout time.Duration) error {
    handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
    if err != nil {
        // 进程可能已经退出
        log.Printf("进程 %d 可能已退出: %v", pid, err)
        return nil
    }
    defer windows.CloseHandle(handle)

    event, err := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
    if event == windows.WAIT_TIMEOUT {
        return fmt.Errorf("进程 %d 未在 %v 内退出", pid, timeout)
    }
    return nil
}

// performUpdate 执行更新操作
func performUpdate(task UpdateTask) error {
    // 1. 备份旧版本
    log.Printf("Step 1: 备份 %s -> %s", task.TargetExe, task.BackupPath)
    if err := os.Rename(task.TargetExe, task.BackupPath); err != nil {
        return fmt.Errorf("备份旧版本失败: %w", err)
    }

    // 2. 复制新版本
    log.Printf("Step 2: 复制 %s -> %s", task.NewExePath, task.TargetExe)
    if err := copyFile(task.NewExePath, task.TargetExe); err != nil {
        return fmt.Errorf("复制新版本失败: %w", err)
    }

    // 3. 验证新文件
    log.Println("Step 3: 验证新版本文件")
    info, err := os.Stat(task.TargetExe)
    if err != nil || info.Size() == 0 {
        return fmt.Errorf("验证新版本失败: %w", err)
    }

    log.Printf("更新完成: 新文件大小 = %d bytes", info.Size())
    return nil
}

// rollback 回滚更新（静默，不弹窗）
func rollback(task UpdateTask) {
    log.Println("执行回滚操作...")
    if _, err := os.Stat(task.BackupPath); err == nil {
        os.Remove(task.TargetExe)
        if err := os.Rename(task.BackupPath, task.TargetExe); err != nil {
            log.Printf("回滚失败: %v", err)
        } else {
            log.Println("回滚成功")
        }
    }
}

// copyFile 复制文件
func copyFile(src, dst string) error {
    source, err := os.Open(src)
    if err != nil {
        return err
    }
    defer source.Close()

    dest, err := os.Create(dst)
    if err != nil {
        return err
    }
    defer dest.Close()

    _, err = io.Copy(dest, source)
    return err
}
```

#### 文件：`cmd/updater/updater.exe.manifest`

```xml
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity
    version="1.0.0.0"
    processorArchitecture="amd64"
    name="CodeSwitch.Updater"
    type="win32"
  />
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="requireAdministrator" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
</assembly>
```

### 4.2 修改 updateservice.go

#### 4.2.1 修改 `findPlatformAsset` 方法

**位置**：`services/updateservice.go` 第 229-236 行

**修改内容**：安装版也下载核心 exe，而非安装器

```go
case "windows":
    // 统一下载核心 exe（无论便携版还是安装版）
    // 安装版通过 updater.exe 提权替换
    targetName = "CodeSwitch.exe"
```

#### 4.2.2 新增 `applyInstalledUpdate` 方法

```go
// applyInstalledUpdate 安装版更新逻辑
func (us *UpdateService) applyInstalledUpdate(newExePath string) error {
    currentExe, err := os.Executable()
    if err != nil {
        return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
    }
    currentExe, _ = filepath.EvalSymlinks(currentExe)

    // 1. 获取或下载 updater.exe
    updaterPath := filepath.Join(us.updateDir, "updater.exe")
    if _, err := os.Stat(updaterPath); os.IsNotExist(err) {
        if err := us.downloadUpdater(updaterPath); err != nil {
            return fmt.Errorf("下载更新器失败: %w", err)
        }
    }

    // 2. 创建更新任务配置
    taskFile := filepath.Join(us.updateDir, "update-task.json")
    task := map[string]interface{}{
        "main_pid":      os.Getpid(),
        "target_exe":    currentExe,
        "new_exe_path":  newExePath,
        "backup_path":   currentExe + ".old",
        "cleanup_paths": []string{
            newExePath,
            filepath.Join(filepath.Dir(us.stateFile), ".pending-update"),
        },
    }

    taskData, _ := json.MarshalIndent(task, "", "  ")
    if err := os.WriteFile(taskFile, taskData, 0o644); err != nil {
        return fmt.Errorf("写入任务配置失败: %w", err)
    }

    // 3. 删除 pending 标记
    pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
    _ = os.Remove(pendingFile)

    // 4. 启动 updater.exe（会自动请求 UAC）
    log.Printf("[UpdateService] 启动更新器: %s %s", updaterPath, taskFile)
    cmd := exec.Command(updaterPath, taskFile)
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("启动更新器失败: %w", err)
    }

    // 5. 退出主程序
    log.Println("[UpdateService] 更新器已启动，准备退出主程序...")
    os.Exit(0)
    return nil
}

// downloadUpdater 从 GitHub Release 下载 updater.exe
func (us *UpdateService) downloadUpdater(targetPath string) error {
    url := fmt.Sprintf("https://github.com/Rogers-F/code-switch-R/releases/download/%s/updater.exe", us.latestVersion)

    log.Printf("[UpdateService] 下载更新器: %s", url)

    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        return fmt.Errorf("HTTP %d", resp.StatusCode)
    }

    out, err := os.Create(targetPath)
    if err != nil {
        return err
    }
    defer out.Close()

    _, err = io.Copy(out, resp.Body)
    return err
}
```

#### 4.2.3 修改 `applyUpdateWindows` 方法

**位置**：第 418-440 行

```go
func (us *UpdateService) applyUpdateWindows(updatePath string) error {
    if us.isPortable {
        return us.applyPortableUpdate(updatePath)
    }
    // 安装版：使用 updater.exe 辅助程序
    return us.applyInstalledUpdate(updatePath)
}
```

### 4.3 修改构建流程

#### 4.3.1 修改 Taskfile.yml

添加 updater 构建任务：

```yaml
build:updater:
  summary: Build updater.exe with UAC manifest
  cmds:
    - go install github.com/akavel/rsrc@latest
    - cmd: cd cmd/updater && rsrc -manifest updater.exe.manifest -o rsrc_windows_amd64.syso
    - cmd: cd cmd/updater && go build -ldflags="-w -s -H windowsgui" -o ../../bin/updater.exe .
  env:
    GOOS: windows
    GOARCH: amd64
    CGO_ENABLED: 0
```

#### 4.3.2 修改 scripts/publish_release.sh

发布资源列表添加 `updater.exe`：

```bash
ASSETS=(
  "${MAC_ZIPS[@]}"
  "bin/codeswitch-amd64-installer.exe"  # 保留，供首次安装
  "bin/CodeSwitch.exe"                   # 更新用
  "bin/updater.exe"                       # 新增
)
```

## 五、更新流程图

```
每日 8:00 定时检查 / 用户手动检查
        ↓
    发现新版本
        ↓
后台下载 CodeSwitch.exe (~10MB)
        ↓
    下载完成
        ↓
弹窗询问 "是否立即更新？"
        ↓
    用户点击确认
        ↓
下载 updater.exe（如果本地没有）
        ↓
创建 update-task.json
        ↓
启动 updater.exe（触发 UAC 确认）
        ↓
    主程序退出
        ↓
=== updater.exe 接管 ===
        ↓
等待主程序 PID 退出（30秒超时）
        ↓
备份 CodeSwitch.exe → CodeSwitch.exe.old
        ↓
复制新版本到 Program Files
        ↓
启动新版本 ✓
        ↓
清理临时文件
```

## 六、需要修改的文件清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `cmd/updater/main.go` | 新建 | updater 主程序（~150行） |
| `cmd/updater/updater.exe.manifest` | 新建 | UAC 权限声明 |
| `services/updateservice.go` | 修改 | 核心更新逻辑 |
| `Taskfile.yml` | 修改 | 添加 updater 构建任务 |
| `scripts/publish_release.sh` | 修改 | 发布 updater.exe |

## 七、风险与回滚

| 机制 | 说明 |
|------|------|
| **备份保留** | 旧版本备份为 `.old` 文件，可手动恢复 |
| **静默回滚** | 更新失败时自动恢复，不弹窗打扰用户 |
| **日志追踪** | `~/.code-switch/updates/update.log` 记录详细过程 |
| **超时保护** | 等待主程序退出有 30 秒超时，防止卡死 |

## 八、用户确认的决策

- **UAC 确认**：可以接受一次 UAC 权限确认框
- **失败处理**：静默回滚，不弹窗提示用户

## 九、测试计划

实现时需同步编写以下测试用例：

### 9.1 单元测试

| 测试项 | 文件 | 说明 |
|--------|------|------|
| `TestFindPlatformAsset_Windows` | `updateservice_test.go` | 验证安装版/便携版都返回 `CodeSwitch.exe` |
| `TestDetectPortableMode` | `updateservice_test.go` | 验证路径检测逻辑 |
| `TestUpdateTaskJSON` | `updateservice_test.go` | 验证任务配置 JSON 格式 |

### 9.2 集成测试（手动）

| 场景 | 预期结果 |
|------|---------|
| 安装版更新成功 | UAC 确认 → 关闭 → 新版本启动 |
| 安装版更新失败 | 静默回滚，旧版本正常运行 |
| 便携版更新 | 无 UAC，直接替换重启 |
| updater.exe 下载失败 | 返回错误，不退出主程序 |

### 9.3 边界条件

- 主程序 30 秒内未退出 → updater 超时退出
- 备份文件已存在 → 覆盖备份
- 新版本文件损坏（0 字节）→ 回滚

## 十、实现优先级

```
1. 修复大小写问题（发布脚本 codeswitch.exe → CodeSwitch.exe）
2. 新建 cmd/updater/ 目录和文件
3. 修改 updateservice.go
4. 修改 Taskfile.yml 添加构建任务
5. 修改 publish_release.sh 添加发布
6. 编写测试用例
7. 本地测试验证
8. 发布新版本
```

---

## 十一、补充设计（审查意见响应）

### 11.1 安全校验机制

**问题**：下载文件无 SHA256 校验，易受链路篡改影响。

**完整解决方案**：

#### 1. Release 发布时生成哈希文件

```bash
# publish_release.sh 中添加
sha256sum bin/CodeSwitch.exe > bin/CodeSwitch.exe.sha256
sha256sum bin/updater.exe > bin/updater.exe.sha256

# 发布资产列表
ASSETS=(
  "bin/CodeSwitch.exe"
  "bin/CodeSwitch.exe.sha256"    # 新增
  "bin/updater.exe"
  "bin/updater.exe.sha256"       # 新增
  # ...
)
```

#### 2. 完整的哈希获取与校验流程

```go
// UpdateService 完整校验流程
func (us *UpdateService) downloadAndVerify(assetName string) (string, error) {
    // 1. 下载主文件
    mainURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, us.latestVersion, assetName)
    mainPath := filepath.Join(us.updateDir, assetName)
    if err := us.downloadFile(mainURL, mainPath, nil); err != nil {
        return "", fmt.Errorf("下载 %s 失败: %w", assetName, err)
    }

    // 2. 下载哈希文件
    hashURL := mainURL + ".sha256"
    hashPath := mainPath + ".sha256"
    if err := us.downloadFile(hashURL, hashPath, nil); err != nil {
        os.Remove(mainPath) // 清理已下载的主文件
        return "", fmt.Errorf("下载哈希文件失败: %w", err)
    }

    // 3. 解析哈希文件（格式: "hash  filename"）
    hashContent, _ := os.ReadFile(hashPath)
    expectedHash := strings.Fields(string(hashContent))[0]
    os.Remove(hashPath) // 哈希文件用完即删

    // 4. 校验主文件
    if err := us.verifyDownload(mainPath, expectedHash); err != nil {
        os.Remove(mainPath)
        return "", err
    }

    return mainPath, nil
}

func (us *UpdateService) verifyDownload(filePath, expectedHash string) error {
    f, err := os.Open(filePath)
    if err != nil {
        return err
    }
    defer f.Close()

    h := sha256.New()
    io.Copy(h, f)
    actual := hex.EncodeToString(h.Sum(nil))

    if !strings.EqualFold(actual, expectedHash) {
        return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedHash, actual)
    }
    log.Printf("[Update] SHA256 校验通过: %s", filePath)
    return nil
}
```

#### 3. 校验失败处理

| 场景 | 处理 |
|------|------|
| 哈希文件下载失败 | 删除主文件，返回错误，前端提示"校验文件获取失败" |
| 哈希不匹配 | 删除主文件，返回错误，前端提示"文件校验失败，请重试" |
| 哈希格式错误 | 同上 |

#### 4. 需要校验的文件

| 文件 | 校验时机 |
|------|---------|
| `CodeSwitch.exe` | 下载完成后、写入 pending 前 |
| `updater.exe` | 首次下载后、启动前 |

### 11.2 完整回滚策略

**问题**：仅覆盖"复制失败"，未覆盖 updater 崩溃、备份占用、重启失败。

**完整场景覆盖**：

| 场景 | 检测方式 | 处理策略 |
|------|---------|---------|
| 复制失败 | `copyFile` 返回错误 | 立即回滚，恢复备份 |
| 新文件 0 字节 | `os.Stat` 检查大小 | 立即回滚，恢复备份 |
| updater 崩溃/被杀 | 主程序启动时检查 `.old` 文件存在但新版本未运行 | 主程序启动时自动恢复 |
| 备份文件被占用 | `os.Rename` 返回错误 | 尝试强制删除或重命名为 `.old.1` |
| 重启新进程失败 | `cmd.Start()` 返回错误 | 回滚后记录日志，下次启动提示用户 |

**主程序启动时的恢复检查**（修正版）：

**逻辑说明**：
1. 检测 `.old` 备份文件是否存在
2. 若存在，校验当前 exe 是否可用（大小 > 1MB）
3. 若可用 → 更新成功，仅清理备份
4. 若不可用 → 更新失败，回滚恢复 `.old`

```go
// main.go 启动时调用
func checkAndRecoverFromFailedUpdate() {
    currentExe, _ := os.Executable()
    backupPath := currentExe + ".old"

    // 检查备份是否存在
    backupInfo, err := os.Stat(backupPath)
    if err != nil {
        return // 无备份，正常情况
    }

    // 检查当前 exe 是否可用（大小 > 0 且可执行）
    currentInfo, err := os.Stat(currentExe)
    currentOK := err == nil && currentInfo.Size() > 1024*1024 // 至少 1MB

    if currentOK {
        // 当前版本正常，说明更新成功，清理备份
        log.Println("[Recovery] 更新成功，清理旧版本备份")
        os.Remove(backupPath)
    } else {
        // 当前版本损坏，需要回滚
        log.Printf("[Recovery] 当前版本异常（size=%d），从备份恢复", currentInfo.Size())
        os.Remove(currentExe)
        if err := os.Rename(backupPath, currentExe); err != nil {
            log.Printf("[Recovery] 回滚失败: %v", err)
            // 最后手段：提示用户手动恢复
        } else {
            log.Println("[Recovery] 回滚成功，已恢复到旧版本")
        }
    }

    _ = backupInfo // 避免未使用警告
}
```

### 11.3 路径判定策略

**问题**：`detectPortableMode` 仅检查 "program files/appdata"，自定义路径可能误判。

**采用方案**：写权限检测（更准确，替代旧的路径字符串匹配）

```go
// 最终实现版本（替代现有 updateservice.go:106-128 的旧逻辑）
func (us *UpdateService) detectPortableMode() bool {
    if runtime.GOOS != "windows" {
        return false
    }

    exePath, err := os.Executable()
    if err != nil {
        return false
    }
    exePath, _ = filepath.EvalSymlinks(exePath)
    exeDir := filepath.Dir(exePath)

    // 方案：直接检测写权限（比路径匹配更准确）
    // 如果能在 exe 所在目录创建文件，则为便携版
    testFile := filepath.Join(exeDir, ".write-test-"+strconv.Itoa(os.Getpid()))
    f, err := os.Create(testFile)
    if err != nil {
        // 无写权限，视为安装版（需要 UAC）
        log.Printf("[Update] 检测为安装版: 无法写入 %s", exeDir)
        return false
    }
    f.Close()
    os.Remove(testFile)

    log.Printf("[Update] 检测为便携版: 可写入 %s", exeDir)
    return true
}
```

**设计假设**：
- 安装版 = 安装在需要 UAC 权限的目录（无法直接写入）
- 便携版 = 安装在用户可写的目录（可以直接写入）

**为什么不用路径字符串匹配**：
- 用户可能安装在 `D:\Apps\CodeSwitch\`（不含 program files）
- 用户可能有特殊权限配置
- 写权限检测是最直接的判断方式

**边界情况**：
| 场景 | 检测结果 | 处理 |
|------|---------|------|
| `C:\Program Files\CodeSwitch\` | 安装版（无写权限） | 使用 updater + UAC |
| `D:\Apps\CodeSwitch\` + 无写权限 | 安装版 | 使用 updater + UAC |
| `D:\Apps\CodeSwitch\` + 有写权限 | 便携版 | 直接替换 |
| `%USERPROFILE%\Desktop\CodeSwitch\` | 便携版（有写权限） | 直接替换 |

### 11.4 超时参数化

**问题**：30 秒固定值可能不够。

**强制要求**：
> **任务 JSON 必须携带 `timeout_sec` 字段，`updater.exe` 的 `waitForProcessExit` 必须使用此值，禁止硬编码 30 秒。**

**解决方案**：

**1. UpdateTask 扩展（任务文件必须包含 timeout_sec）**：
```go
// UpdateTask 完整定义
type UpdateTask struct {
    MainPID      int      `json:"main_pid"`
    TargetExe    string   `json:"target_exe"`
    NewExePath   string   `json:"new_exe_path"`
    BackupPath   string   `json:"backup_path"`
    CleanupPaths []string `json:"cleanup_paths"`
    TimeoutSec   int      `json:"timeout_sec"` // 必填，等待超时（秒）
}
```

**2. UpdateService 写入任务 JSON 时计算超时**：
```go
// applyInstalledUpdate 中
fileInfo, _ := os.Stat(newExePath)
timeout := calculateTimeout(fileInfo.Size())

task := map[string]interface{}{
    "main_pid":      os.Getpid(),
    "target_exe":    currentExe,
    "new_exe_path":  newExePath,
    "backup_path":   currentExe + ".old",
    "cleanup_paths": []string{newExePath},
    "timeout_sec":   timeout,  // 动态计算
}

func calculateTimeout(fileSize int64) int {
    base := 30
    extra := int(fileSize / (100 * 1024 * 1024)) * 10 // 每 100MB +10s
    return base + extra
}
```

**3. updater.exe 使用任务文件中的超时值**：
```go
// waitForProcessExit 使用 task.TimeoutSec
func main() {
    // ...
    timeout := time.Duration(task.TimeoutSec) * time.Second
    if task.TimeoutSec <= 0 {
        timeout = 30 * time.Second // 兜底默认值
    }
    if err := waitForProcessExit(task.MainPID, timeout); err != nil {
        log.Fatalf("等待主程序退出超时（%ds）: %v", task.TimeoutSec, err)
    }
}
```

### 11.5 残留文件清理策略

**问题**：旧安装器、旧 updater、旧备份文件累积。

**清理策略**（完整实现）：

```go
// 主程序启动时清理（在 main.go 初始化阶段调用）
func cleanupOldFiles() {
    updateDir := filepath.Join(os.Getenv("USERPROFILE"), ".code-switch", "updates")

    // 1. 清理超过 7 天的备份文件
    cleanupByAge(updateDir, ".old", 7*24*time.Hour)

    // 2. 清理旧版本下载文件（保留最新 1 个）
    cleanupByCount(updateDir, "CodeSwitch*.exe", 1)

    // 3. 清理旧日志（保留最近 5 个，或总大小 < 5MB）
    cleanupLogs(updateDir, 5, 5*1024*1024)

    // 4. 清理旧 updater（保留最新 1 个）
    cleanupByCount(updateDir, "updater*.exe", 1)
}

// 按时间清理
func cleanupByAge(dir, suffix string, maxAge time.Duration) {
    filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() {
            return nil
        }
        if strings.HasSuffix(path, suffix) && time.Since(info.ModTime()) > maxAge {
            log.Printf("[Cleanup] 删除过期文件: %s (age=%v)", path, time.Since(info.ModTime()))
            os.Remove(path)
        }
        return nil
    })
}

// 按数量清理（保留最新 N 个）
func cleanupByCount(dir, pattern string, keepCount int) {
    matches, _ := filepath.Glob(filepath.Join(dir, pattern))
    if len(matches) <= keepCount {
        return
    }

    // 按修改时间排序（新→旧）
    sort.Slice(matches, func(i, j int) bool {
        infoI, _ := os.Stat(matches[i])
        infoJ, _ := os.Stat(matches[j])
        return infoI.ModTime().After(infoJ.ModTime())
    })

    // 删除多余的旧文件
    for _, path := range matches[keepCount:] {
        log.Printf("[Cleanup] 删除旧版本: %s", path)
        os.Remove(path)
    }
}

// 日志清理（数量 + 大小双重限制）
func cleanupLogs(dir string, maxCount int, maxTotalSize int64) {
    pattern := filepath.Join(dir, "update*.log")
    matches, _ := filepath.Glob(pattern)
    if len(matches) == 0 {
        return
    }

    // 按修改时间排序（新→旧）
    sort.Slice(matches, func(i, j int) bool {
        infoI, _ := os.Stat(matches[i])
        infoJ, _ := os.Stat(matches[j])
        return infoI.ModTime().After(infoJ.ModTime())
    })

    var totalSize int64
    for i, path := range matches {
        info, _ := os.Stat(path)

        // 超过数量限制或大小限制，删除
        if i >= maxCount || totalSize+info.Size() > maxTotalSize {
            log.Printf("[Cleanup] 删除旧日志: %s (size=%d)", path, info.Size())
            os.Remove(path)
        } else {
            totalSize += info.Size()
        }
    }
}
```

**清理时机**：
- 主程序启动时（`main.go` 初始化阶段）
- 更新下载完成后（可选）

**清理规则汇总**：

| 文件类型 | 保留规则 | 清理条件 |
|---------|---------|---------|
| `.old` 备份 | 最多 7 天 | 超过 7 天自动删除 |
| `CodeSwitch*.exe` | 最新 1 个 | 多余的旧版本删除 |
| `updater*.exe` | 最新 1 个 | 多余的旧版本删除 |
| `update*.log` | 最新 5 个且 < 5MB | 超出限制删除 |

### 11.6 多用户会话隔离与并发互斥

**问题**：多用户环境下更新状态可能冲突；同一用户多实例并发更新可能冲突。

**解决方案**：

#### 1. 用户级目录隔离
- 所有状态文件使用 `%USERPROFILE%\.code-switch\`（用户级目录）
- 不使用 `%PROGRAMDATA%` 或其他共享目录
- 每个用户独立的更新状态、日志、临时文件

**当前设计已符合**：`~/.code-switch/` 即 `%USERPROFILE%\.code-switch\`

#### 2. 同用户多实例互斥（锁文件机制）

```go
// 更新开始前获取锁
func (us *UpdateService) acquireUpdateLock() error {
    lockPath := filepath.Join(us.updateDir, "update.lock")

    // 尝试创建锁文件（排他模式）
    f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
    if err != nil {
        if os.IsExist(err) {
            // 检查锁文件是否过期（超过 10 分钟视为死锁）
            info, _ := os.Stat(lockPath)
            if time.Since(info.ModTime()) > 10*time.Minute {
                os.Remove(lockPath)
                return us.acquireUpdateLock() // 重试
            }
            return fmt.Errorf("另一个更新正在进行中")
        }
        return err
    }

    // 写入 PID 和时间戳
    fmt.Fprintf(f, "%d\n%s", os.Getpid(), time.Now().Format(time.RFC3339))
    f.Close()

    us.lockFile = lockPath
    return nil
}

// 更新完成后释放锁
func (us *UpdateService) releaseUpdateLock() {
    if us.lockFile != "" {
        os.Remove(us.lockFile)
        us.lockFile = ""
    }
}
```

**互斥规则**：
| 场景 | 处理 |
|------|------|
| 锁文件存在且 < 10 分钟 | 返回"另一个更新正在进行中"，前端提示等待 |
| 锁文件存在且 > 10 分钟 | 视为死锁，删除锁文件并重试 |
| 更新成功/失败 | 释放锁文件 |
| 进程崩溃 | 下次启动时 10 分钟后自动清理 |

### 11.7 前端交互规范

**问题**：弹窗内容、失败提示、重试入口未定义。

**交互规范**：

#### 发现新版本弹窗
```
标题：发现新版本 v0.5.0
内容：
  当前版本：v0.4.0
  新版本：v0.5.0
  更新内容：[从 Release Notes 获取]

  点击"立即更新"将下载更新包（约 10MB）。
  下载完成后需要重启应用。

按钮：[稍后提醒] [立即更新]
```

#### 下载进度
```
标题：正在下载更新...
内容：
  下载进度：45% (4.5MB / 10MB)
  预计剩余：30 秒

按钮：[取消]
```

#### 下载完成
```
标题：更新已就绪
内容：
  新版本 v0.5.0 已下载完成。
  点击"立即重启"将关闭应用并安装更新。
  （安装版需要确认一次管理员权限）

按钮：[稍后重启] [立即重启]
```

#### 更新失败
```
标题：更新失败
内容：
  更新过程中发生错误：[错误信息]

  应用已自动恢复到之前的版本。
  您可以稍后重试，或手动下载安装包。

按钮：[查看日志] [重试] [关闭]
```

#### 设置页入口
```
设置 > 关于
  当前版本：v0.4.0
  [检查更新] 按钮

  自动更新：[开关]
  - 开启后每天 8:00 自动检查并下载更新
```

#### 错误码与文案映射

| 错误码 | 后端错误 | 前端文案 |
|--------|---------|---------|
| `ERR_DOWNLOAD_FAILED` | 下载文件失败 | "下载失败，请检查网络连接后重试" |
| `ERR_HASH_DOWNLOAD_FAILED` | 哈希文件下载失败 | "校验文件获取失败，请稍后重试" |
| `ERR_HASH_MISMATCH` | SHA256 校验失败 | "文件校验失败，可能已损坏，请重试" |
| `ERR_UPDATE_LOCKED` | 另一个更新正在进行 | "另一个更新正在进行中，请稍后再试" |
| `ERR_UAC_DENIED` | UAC 权限被拒绝 | "需要管理员权限才能更新，请点击确认" |
| `ERR_UPDATER_LAUNCH_FAILED` | 启动 updater 失败 | "启动更新程序失败，请手动下载安装包" |
| `ERR_TIMEOUT` | 等待超时 | "更新超时，请手动重启应用" |
| `ERR_ROLLBACK_FAILED` | 回滚失败 | "更新失败且无法恢复，请重新安装应用" |

**后端返回格式**：
```go
type UpdateError struct {
    Code    string `json:"code"`    // 错误码
    Message string `json:"message"` // 技术描述（用于日志）
}
```

**前端处理**：
```typescript
function getErrorMessage(code: string): string {
    const messages: Record<string, string> = {
        'ERR_DOWNLOAD_FAILED': '下载失败，请检查网络连接后重试',
        'ERR_HASH_MISMATCH': '文件校验失败，可能已损坏，请重试',
        // ...
    }
    return messages[code] || '更新失败，请稍后重试'
}
```

### 11.8 版本兼容性规范

**问题**：updater 与主程序的兼容/回退策略未定义。

**版本兼容规则**：

| 场景 | 处理 |
|------|------|
| updater 版本 < 主程序要求 | 重新下载最新 updater |
| 主程序版本跨大版本升级 | 降级到安装器模式（调用 NSIS） |
| 任务文件格式不兼容 | updater 返回错误，主程序回退到安装器模式 |

**最低版本约束**：

```go
// UpdateTask 添加版本字段
type UpdateTask struct {
    // ...
    UpdaterMinVersion string `json:"updater_min_version"` // 要求的 updater 最低版本
    AppVersion        string `json:"app_version"`         // 目标应用版本
}

// updater 启动时检查
func checkVersionCompatibility(task UpdateTask) error {
    currentUpdaterVersion := "1.0.0"
    if semver.Compare(currentUpdaterVersion, task.UpdaterMinVersion) < 0 {
        return fmt.Errorf("updater 版本过低: 当前 %s, 要求 %s",
            currentUpdaterVersion, task.UpdaterMinVersion)
    }
    return nil
}
```

**回退策略**：
1. updater 版本不兼容 → 删除旧 updater，重新下载
2. 下载失败 → 回退到 NSIS 安装器模式
3. 任务文件解析失败 → 记录日志，回退到安装器模式

### 11.9 资产命名约束（强调）

**问题**：大小写不一致可能导致下载失败。

**约束规范**（必须在所有相关位置保持一致）：

| 位置 | 文件名 | 说明 |
|------|--------|------|
| 构建输出 | `CodeSwitch.exe` | 大写 C 和 S |
| 发布脚本 | `bin/CodeSwitch.exe` | 必须与构建输出一致 |
| `findPlatformAsset` | `"CodeSwitch.exe"` | 精确匹配 |
| GitHub Release | `CodeSwitch.exe` | 上传时保持一致 |

**修改清单**：

1. **Taskfile.yml**（构建输出）：
   ```yaml
   go build -o bin/CodeSwitch.exe  # 大写
   ```

2. **publish_release.sh**：
   ```bash
   ASSETS=(
     "bin/CodeSwitch.exe"           # 大写，与构建一致
     "bin/CodeSwitch.exe.sha256"
     "bin/updater.exe"
     "bin/updater.exe.sha256"
   )
   ```

3. **updateservice.go**：
   ```go
   case "windows":
       targetName = "CodeSwitch.exe"  // 大写
   ```

**验证方法**：
```bash
# 构建后检查
ls -la bin/ | grep -i codeswitch
# 应输出: CodeSwitch.exe（大写）
```
