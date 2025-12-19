# Code-Switch Linux 支持方案（修订版）

**文档版本**: v2.1
**创建日期**: 2025-12-01
**修订日期**: 2025-12-01
**作者**: Half open flowers
**状态**: 待审核

---

## 一、执行摘要

### 1.1 目标

为 Code-Switch 应用添加完整的 Linux 平台支持，包括：
1. GitHub Actions 自动构建 Linux 产物
2. 发布到 GitHub Release 供用户下载
3. 自动更新支持（带 SHA256 校验）

### 1.2 现状评估（修正）

| 模块 | 完成度 | 说明 |
|------|--------|------|
| **Taskfile 构建配置** | 100% | **4 种打包格式**（AppImage/DEB/RPM/AUR） |
| **nFPM 包配置** | 80% | 版本号硬编码、postremove 未挂载 |
| **AppImage 脚本** | 100% | 支持双架构 |
| **后端代码适配** | 85% | 更新机制缺少 SHA256 校验 |
| **GitHub Actions CI** | 0% | **完全缺失** |

### 1.3 关键修正项

| 问题类型 | 数量 | 详见章节 |
|---------|------|---------|
| 阻断级 | 4 | 三、四、五、六 |
| 高优先级 | 5 | 七 |
| 中优先级 | 3 | 八 |

---

## 二、技术背景

### 2.1 WebKit2GTK 版本矩阵

**关键发现**：Ubuntu 22.04 **没有** `libwebkit2gtk-4.1-dev`，只有 4.0 版本。

| 发行版 | WebKit 版本 | 包名（开发） | 包名（运行时） | 验证来源 |
|--------|-------------|-------------|---------------|---------|
| Ubuntu 22.04 LTS | **4.0** | `libwebkit2gtk-4.0-dev` | `libwebkit2gtk-4.0-37` | 官方仓库 |
| Ubuntu 24.04 LTS | 4.1 | `libwebkit2gtk-4.1-dev` | `libwebkit2gtk-4.1-0` | 官方仓库 |
| Debian 12 (Bookworm) | **4.1** | `libwebkit2gtk-4.1-dev` | `libwebkit2gtk-4.1-0` | [packages.debian.org](https://packages.debian.org/bookworm/libwebkit2gtk-4.1-0) (v2.48.5) |
| Fedora 39/40 | 4.1 | `webkit2gtk4.1-devel` | `webkit2gtk4.1` | 官方仓库 |
| Arch Linux | 4.1 | `webkit2gtk-4.1` | `webkit2gtk-4.1` | 官方仓库 |

**Debian 12 依赖确认**：经 [Debian 官方包索引](https://packages.debian.org/bookworm/libwebkit2gtk-4.1-0) 验证，Debian 12 Bookworm 默认仓库**同时包含 4.0 和 4.1 版本**，当前 4.1 版本为 `2.48.5-1~deb12u1`。方案中的依赖表正确。

### 2.2 决策：CI 环境选择

**推荐方案**：使用 `ubuntu-24.04`

| 选项 | 优点 | 缺点 |
|------|------|------|
| **ubuntu-24.04** (推荐) | WebKit 4.1 原生支持，与目标发行版一致 | 较新 |
| ubuntu-22.04 + 回退 4.0 | 更稳定的 Runner | 需同步修改 nfpm 依赖表 |

### 2.3 现有文件命名约定

| 文件 | 当前值 | 说明 |
|------|--------|------|
| 可执行文件 | `CodeSwitch` | 首字母大写 |
| 安装路径 | `/usr/local/bin/CodeSwitch` | nfpm.yaml:21 |
| Desktop Exec | `/usr/local/bin/CodeSwitch %u` | desktop:6 |
| AppImage | `CodeSwitch.AppImage` | updateservice.go:247 |

**结论**：命名已统一为 `CodeSwitch`（首字母大写），文档错误描述为 `codeswitch`。

---

## 三、阻断级修正 #1：GitHub Actions Linux 构建

### 3.1 新增 build-linux Job

在 `.github/workflows/release.yml` 中添加：

```yaml
build-linux:
  name: Build Linux
  runs-on: ubuntu-24.04  # 关键：使用 24.04 获取 WebKit 4.1
  steps:
    - uses: actions/checkout@v4

    - uses: actions/setup-go@v5
      with:
        go-version: '1.24'

    - uses: actions/setup-node@v4
      with:
        node-version: '22'

    - name: Install Wails
      run: go install github.com/wailsapp/wails/v3/cmd/wails3@latest

    - name: Install Linux Build Dependencies
      run: |
        sudo apt-get update
        sudo apt-get install -y \
          build-essential \
          pkg-config \
          libgtk-3-dev \
          libwebkit2gtk-4.1-dev

    - name: Install frontend dependencies
      run: cd frontend && npm install

    - name: Update build assets
      run: wails3 task common:update:build-assets

    - name: Generate bindings
      run: wails3 task common:generate:bindings

    - name: Build Linux Binary
      run: wails3 task linux:build
      env:
        PRODUCTION: "true"

    - name: Generate Desktop File
      run: wails3 task linux:generate:dotdesktop

    - name: Create AppImage
      run: wails3 task linux:create:appimage

    # 设置 nfpm 脚本权限（见修正 7.2）
    - name: Set nfpm script permissions
      run: |
        chmod +x build/linux/nfpm/scripts/postinstall.sh
        chmod +x build/linux/nfpm/scripts/postremove.sh

    # 注入版本号到 nfpm（见修正 #3）
    - name: Update nfpm version
      run: |
        VERSION=${GITHUB_REF#refs/tags/v}
        sed -i "s/^version:.*/version: \"$VERSION\"/" build/linux/nfpm/nfpm.yaml
        echo "Updated nfpm version to: $VERSION"

    - name: Create DEB Package
      run: wails3 task linux:create:deb

    - name: Create RPM Package
      run: wails3 task linux:create:rpm

    - name: Generate SHA256 Checksums
      run: |
        cd bin
        sha256sum CodeSwitch.AppImage > CodeSwitch.AppImage.sha256
        sha256sum codeswitch_*.deb > codeswitch.deb.sha256 2>/dev/null || true
        sha256sum codeswitch-*.rpm > codeswitch.rpm.sha256 2>/dev/null || true
        echo "SHA256 checksums:"
        cat *.sha256

    - name: Upload artifacts
      uses: actions/upload-artifact@v4
      with:
        name: linux-amd64
        path: |
          bin/CodeSwitch.AppImage
          bin/CodeSwitch.AppImage.sha256
          bin/codeswitch_*.deb
          bin/codeswitch.deb.sha256
          bin/codeswitch-*.rpm
          bin/codeswitch.rpm.sha256
```

### 3.2 关键点说明

| 配置 | 值 | 原因 |
|------|-----|------|
| `runs-on` | `ubuntu-24.04` | WebKit 4.1 原生支持 |
| `libwebkit2gtk-4.1-dev` | 是 | 匹配 24.04 包名 |
| SHA256 生成 | 是 | 与 Windows 保持一致 |

---

## 四、阻断级修正 #2：发布流程补全

### 4.1 修改 create-release Job

```yaml
create-release:
  name: Create Release
  needs: [build-macos, build-windows, build-linux]  # 添加 build-linux
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4

    - name: Download all artifacts
      uses: actions/download-artifact@v4
      with:
        path: artifacts

    - name: Display structure
      run: ls -R artifacts

    - name: Prepare release assets
      run: |
        mkdir -p release-assets

        # macOS
        cp artifacts/macos-arm64/codeswitch-macos-arm64.zip release-assets/
        cp artifacts/macos-amd64/codeswitch-macos-amd64.zip release-assets/

        # Windows
        cp artifacts/windows-amd64/CodeSwitch-amd64-installer.exe release-assets/
        cp artifacts/windows-amd64/CodeSwitch.exe release-assets/
        cp artifacts/windows-amd64/CodeSwitch.exe.sha256 release-assets/
        cp artifacts/windows-amd64/updater.exe release-assets/
        cp artifacts/windows-amd64/updater.exe.sha256 release-assets/

        # Linux（新增）
        cp artifacts/linux-amd64/CodeSwitch.AppImage release-assets/
        cp artifacts/linux-amd64/CodeSwitch.AppImage.sha256 release-assets/
        cp artifacts/linux-amd64/codeswitch_*.deb release-assets/ 2>/dev/null || true
        cp artifacts/linux-amd64/codeswitch.deb.sha256 release-assets/ 2>/dev/null || true
        cp artifacts/linux-amd64/codeswitch-*.rpm release-assets/ 2>/dev/null || true
        cp artifacts/linux-amd64/codeswitch.rpm.sha256 release-assets/ 2>/dev/null || true

        ls -lh release-assets/

    - name: Create Release
      uses: softprops/action-gh-release@v1
      with:
        files: release-assets/*
        body: |
          ## 下载说明

          | 平台 | 文件 | 说明 |
          |------|------|------|
          | **Windows (首次)** | `CodeSwitch-amd64-installer.exe` | NSIS 安装器 |
          | **Windows (便携)** | `CodeSwitch.exe` | 直接运行 |
          | **Windows (更新)** | `updater.exe` | 静默更新辅助 |
          | **macOS (ARM)** | `codeswitch-macos-arm64.zip` | Apple Silicon |
          | **macOS (Intel)** | `codeswitch-macos-amd64.zip` | Intel 芯片 |
          | **Linux (通用)** | `CodeSwitch.AppImage` | 跨发行版便携 |
          | **Linux (Debian/Ubuntu)** | `codeswitch_*.deb` | apt 安装 |
          | **Linux (RHEL/Fedora)** | `codeswitch-*.rpm` | dnf/yum 安装 |

          ## 文件校验

          所有平台均提供 SHA256 校验文件。
        draft: false
        prerelease: false
      env:
        GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

---

## 五、阻断级修正 #3：版本号动态注入

### 5.1 问题

`build/linux/nfpm/nfpm.yaml` 第 9 行硬编码：
```yaml
version: "0.1.0"
```

### 5.2 解决方案

**方案 A（推荐）**：CI 中用 `sed` 替换

已在 3.1 节的 "Update nfpm version" 步骤中实现：

```bash
VERSION=${GITHUB_REF#refs/tags/v}
sed -i "s/^version:.*/version: \"$VERSION\"/" build/linux/nfpm/nfpm.yaml
```

**方案 B（备选）**：使用环境变量

修改 `nfpm.yaml`:
```yaml
version: "${VERSION:-0.1.0}"
```

然后在 CI 中：
```bash
export VERSION=${GITHUB_REF#refs/tags/v}
wails3 task linux:create:deb
```

### 5.3 本地开发兼容

本地执行 `wails3 task linux:package` 时，版本号保持 `0.1.0`（开发标识），不影响正常使用。

---

## 六、阻断级修正 #4：可执行名统一

### 6.1 现状确认

经代码审查，命名**已统一**为 `CodeSwitch`（首字母大写）：

| 位置 | 文件 | 当前值 | 状态 |
|------|------|--------|------|
| nfpm 安装路径 | `nfpm.yaml:21` | `/usr/local/bin/CodeSwitch` | OK |
| Desktop Exec | `desktop:6` | `/usr/local/bin/CodeSwitch %u` | OK |
| AppImage 名 | `updateservice.go:247` | `CodeSwitch.AppImage` | OK |
| 二进制产物 | `Taskfile.yml:19` | `bin/CodeSwitch` | OK |

### 6.2 需要修正的文档

原方案文档中 "命令为 `codeswitch`" 的描述**错误**，实际命令为 `CodeSwitch`。

如果需要小写命令别名，可在 `postinstall.sh` 中添加符号链接：

```bash
# 可选：创建小写别名
ln -sf /usr/local/bin/CodeSwitch /usr/local/bin/codeswitch
```

---

## 七、高优先级修正

### 7.1 ARM64 构建（暂不实施）

**问题分析**：
- CGO 交叉编译需要 ARM64 工具链 + GTK/WebKit ARM64 库
- GitHub Actions 标准 Runner 不支持 ARM64 原生构建
- QEMU 模拟编译极慢（10-30x）

**建议方案**：

| 阶段 | 策略 | 说明 |
|------|------|------|
| **v1.0** | 仅 amd64 | 覆盖 95%+ Linux 桌面用户 |
| **v1.x** | 自托管 ARM64 Runner | 或使用 Oracle Cloud 免费 ARM 实例 |

**在方案中明确标注**：
```
⚠️ ARM64 支持暂不包含在本次迭代中，将在后续版本实现。
```

### 7.2 nFPM 脚本挂载修复

**当前问题**（`nfpm.yaml:46-51`）：
```yaml
scripts:
  postinstall: "./build/linux/nfpm/scripts/postinstall.sh"
  # postremove 被注释掉了
```

**修正后**：
```yaml
scripts:
  postinstall: "./build/linux/nfpm/scripts/postinstall.sh"
  postremove: "./build/linux/nfpm/scripts/postremove.sh"
```

**postinstall.sh 内容**：
```bash
#!/bin/bash
set -e

# 更新桌面数据库
if command -v update-desktop-database &> /dev/null; then
    update-desktop-database /usr/share/applications 2>/dev/null || true
fi

# 更新图标缓存
if command -v gtk-update-icon-cache &> /dev/null; then
    gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
fi

# 可选：创建小写别名
ln -sf /usr/local/bin/CodeSwitch /usr/local/bin/codeswitch 2>/dev/null || true

echo "Code-Switch 安装完成"
```

**postremove.sh 内容**：
```bash
#!/bin/bash
set -e

# 移除符号链接
rm -f /usr/local/bin/codeswitch 2>/dev/null || true

# 移除自启动配置
AUTOSTART="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/codeswitch.desktop"
rm -f "$AUTOSTART" 2>/dev/null || true

# 更新桌面数据库
if command -v update-desktop-database &> /dev/null; then
    update-desktop-database /usr/share/applications 2>/dev/null || true
fi

echo "Code-Switch 已卸载，用户配置保留在 ~/.code-switch"
```

**重要：脚本权限设置**

nFPM 要求脚本文件具有可执行权限，否则构建会失败。需要在 CI 中添加步骤：

```yaml
- name: Set nfpm script permissions
  run: |
    chmod +x build/linux/nfpm/scripts/postinstall.sh
    chmod +x build/linux/nfpm/scripts/postremove.sh
```

或在本地开发时执行：
```bash
chmod +x build/linux/nfpm/scripts/*.sh
git update-index --chmod=+x build/linux/nfpm/scripts/*.sh
```

**提交到 Git 时保留权限**：
```bash
git add --chmod=+x build/linux/nfpm/scripts/postinstall.sh
git add --chmod=+x build/linux/nfpm/scripts/postremove.sh
```

### 7.3 Linux 更新机制增强（SHA256 校验）

**当前代码问题**（`updateservice.go:495-523`）：
- 仅复制 + chmod + 删除备份
- 无 SHA256 校验
- 备份只保留 1 个
- **`UpdateService` 缺少 `latestUpdateInfo` 字段**

**修正方案分为三步**：

#### 步骤 1：添加 `latestUpdateInfo` 字段

修改 `services/updateservice.go` 中的 `UpdateService` 结构体（约第 43-60 行）：

```go
// UpdateService 更新服务
type UpdateService struct {
    currentVersion   string
    latestVersion    string
    downloadURL      string
    updateFilePath   string
    autoCheckEnabled bool
    downloadProgress float64
    dailyCheckTimer  *time.Timer
    lastCheckTime    time.Time
    checkFailures    int
    updateReady      bool
    isPortable       bool
    mu               sync.Mutex
    stateFile        string
    updateDir        string
    lockFile         string

    // 新增：保存最新检查到的更新信息（含 SHA256）
    latestUpdateInfo *UpdateInfo
}
```

#### 步骤 2：修改 `CheckUpdate` 解析 SHA256

修改 `services/updateservice.go` 中的 `CheckUpdate` 方法（约第 192-210 行）：

```go
// 查找当前平台的下载链接
downloadURL := us.findPlatformAsset(release.Assets)
if downloadURL == "" {
    log.Printf("[UpdateService] ❌ 未找到适用于 %s 的安装包", runtime.GOOS)
    return nil, fmt.Errorf("未找到适用于 %s 的安装包", runtime.GOOS)
}

// 新增：查找对应的 SHA256 校验文件
sha256Hash := us.findSHA256ForAsset(release.Assets, downloadURL)

log.Printf("[UpdateService] 下载链接: %s", downloadURL)
if sha256Hash != "" {
    log.Printf("[UpdateService] SHA256: %s", sha256Hash)
}

updateInfo := &UpdateInfo{
    Available:    needUpdate,
    Version:      release.TagName,
    DownloadURL:  downloadURL,
    ReleaseNotes: release.Body,
    SHA256:       sha256Hash,  // 新增
}

us.mu.Lock()
us.latestVersion = release.TagName
us.downloadURL = downloadURL
us.latestUpdateInfo = updateInfo  // 新增：保存更新信息
us.mu.Unlock()

return updateInfo, nil
```

#### 步骤 3：添加 SHA256 解析辅助函数

在 `services/updateservice.go` 中添加：

```go
// findSHA256ForAsset 查找资产对应的 SHA256 哈希
// SHA256 文件格式：<hash>  <filename>
func (us *UpdateService) findSHA256ForAsset(assets []struct {
    Name               string `json:"name"`
    BrowserDownloadURL string `json:"browser_download_url"`
    Size               int64  `json:"size"`
}, assetURL string) string {
    // 从 URL 提取文件名
    assetName := filepath.Base(assetURL)
    sha256FileName := assetName + ".sha256"

    // 查找 SHA256 文件
    var sha256URL string
    for _, asset := range assets {
        if asset.Name == sha256FileName {
            sha256URL = asset.BrowserDownloadURL
            break
        }
    }

    if sha256URL == "" {
        log.Printf("[UpdateService] 未找到 SHA256 文件: %s", sha256FileName)
        return ""
    }

    // 下载并解析 SHA256 文件
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Get(sha256URL)
    if err != nil {
        log.Printf("[UpdateService] 下载 SHA256 文件失败: %v", err)
        return ""
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        log.Printf("[UpdateService] SHA256 文件返回错误状态码: %d", resp.StatusCode)
        return ""
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        log.Printf("[UpdateService] 读取 SHA256 文件失败: %v", err)
        return ""
    }

    // 解析格式：<hash>  <filename> 或 <hash> <filename>
    content := strings.TrimSpace(string(body))
    parts := strings.Fields(content)
    if len(parts) >= 1 {
        return parts[0]  // 返回哈希值
    }

    return ""
}
```

#### 步骤 4：修改 `applyUpdateLinux` 使用 SHA256 校验

修改 `services/updateservice.go` 中的 `applyUpdateLinux` 方法：

```go
// applyUpdateLinux Linux 平台更新（增强版）
func (us *UpdateService) applyUpdateLinux(appImagePath string) error {
    // 1. SHA256 校验
    us.mu.Lock()
    var expectedHash string
    if us.latestUpdateInfo != nil {
        expectedHash = us.latestUpdateInfo.SHA256
    }
    us.mu.Unlock()

    if expectedHash != "" {
        actualHash, err := calculateFileSHA256(appImagePath)
        if err != nil {
            return fmt.Errorf("计算 SHA256 失败: %w", err)
        }
        if !strings.EqualFold(actualHash, expectedHash) {
            return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedHash, actualHash)
        }
        log.Println("[UpdateService] SHA256 校验通过")
    }

    // 2. ELF 格式校验
    f, err := os.Open(appImagePath)
    if err != nil {
        return fmt.Errorf("无法打开 AppImage: %w", err)
    }
    magic := make([]byte, 4)
    _, err = f.Read(magic)
    f.Close()
    if err != nil || magic[0] != 0x7F || magic[1] != 'E' || magic[2] != 'L' || magic[3] != 'F' {
        return fmt.Errorf("无效的 AppImage 格式（非 ELF）")
    }

    // 3. 获取当前可执行文件路径
    currentExe, err := os.Executable()
    if err != nil {
        return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
    }
    currentExe, _ = filepath.EvalSymlinks(currentExe)

    // 4. 带时间戳的备份（保留最近 2 个）
    timestamp := time.Now().Format("20060102-150405")
    backupPath := currentExe + ".backup-" + timestamp
    if err := copyUpdateFile(currentExe, backupPath); err != nil {
        log.Printf("[UpdateService] 备份失败（继续）: %v", err)
    }

    // 5. 替换可执行文件
    if err := copyUpdateFile(appImagePath, currentExe); err != nil {
        // 尝试恢复
        _ = copyUpdateFile(backupPath, currentExe)
        return fmt.Errorf("替换失败: %w", err)
    }

    // 6. 设置可执行权限
    if err := os.Chmod(currentExe, 0o755); err != nil {
        return fmt.Errorf("设置执行权限失败: %w", err)
    }

    // 7. 清理旧备份（保留最近 2 个）
    us.cleanupOldBackups(filepath.Dir(currentExe), "*.backup-*", 2)

    log.Println("[UpdateService] Linux 更新应用成功")
    return nil
}
```

**需要添加的辅助函数**：

```go
// cleanupOldBackups 清理旧备份文件，保留最近 n 个
func (us *UpdateService) cleanupOldBackups(dir, pattern string, keep int) {
    matches, _ := filepath.Glob(filepath.Join(dir, pattern))
    if len(matches) <= keep {
        return
    }

    // 按修改时间排序
    sort.Slice(matches, func(i, j int) bool {
        fi, _ := os.Stat(matches[i])
        fj, _ := os.Stat(matches[j])
        return fi.ModTime().After(fj.ModTime())
    })

    // 删除旧的
    for _, f := range matches[keep:] {
        os.Remove(f)
        log.Printf("[UpdateService] 清理旧备份: %s", f)
    }
}
```

### 7.4 清理逻辑平台隔离

**当前问题**（`main.go:400-431`）：

1. 函数签名是 `cleanupOldFiles()` **无参数**（内部自行获取 updateDir）
2. 第一行 `if runtime.GOOS != "windows" { return }` **直接跳过非 Windows 平台**
3. 内部只清理 `.exe` 文件

**修正方案**：

修改 `main.go` 中的 `cleanupOldFiles` 函数（约第 400-431 行）：

```go
// cleanupOldFiles 在主程序启动时调用 - 支持所有平台
func cleanupOldFiles() {
    // 移除原有的 Windows 判断：
    // if runtime.GOOS != "windows" {
    //     return
    // }

    home, err := os.UserHomeDir()
    if err != nil {
        return
    }

    updateDir := filepath.Join(home, ".code-switch", "updates")
    if _, err := os.Stat(updateDir); os.IsNotExist(err) {
        return // 更新目录不存在
    }

    log.Printf("[Cleanup] 开始清理更新目录: %s", updateDir)

    // 1. 清理超过 7 天的 .old 备份文件（所有平台通用）
    cleanupByAge(updateDir, ".old", 7*24*time.Hour)

    // 2. 按平台清理旧版本下载文件
    switch runtime.GOOS {
    case "windows":
        cleanupByCount(updateDir, "CodeSwitch*.exe", 1)
        cleanupByCount(updateDir, "updater*.exe", 1)
    case "linux":
        cleanupByCount(updateDir, "CodeSwitch*.AppImage", 1)
    case "darwin":
        cleanupByCount(updateDir, "codeswitch-macos-*.zip", 1)
    }

    // 3. 清理旧日志（保留最近 5 个，或总大小 < 5MB）- 所有平台通用
    cleanupLogs(updateDir, 5, 5*1024*1024)

    log.Println("[Cleanup] 清理完成")
}
```

**确保调用**：在 `main()` 函数中（约第 160-180 行附近），确保 `cleanupOldFiles()` 在所有平台都被调用：

```go
func main() {
    // ... 初始化代码 ...

    // Windows 特定：恢复失败的更新
    if runtime.GOOS == "windows" {
        recoverFromFailedUpdate()
    }

    // 所有平台：清理旧文件（移除原有的 Windows 判断）
    cleanupOldFiles()

    // ... 其余初始化代码 ...
}
```

**验证点**：
- 确认 `main()` 中 `cleanupOldFiles()` 调用不在任何 `if runtime.GOOS == "windows"` 分支内
- 当前代码已在 `main.go` 约第 160 行位置调用，需确认调用位置正确

### 7.5 AUR 打包风险声明

**问题**：
- `wails3 tool package -format archlinux` 生成的是 `.tar.zst` 包
- 不是标准 PKGBUILD，不能直接提交到 AUR
- 没有 AUR 仓库维护计划

**方案**：

1. **本次迭代**：不在 GitHub Release 发布 AUR 包
2. **Taskfile 保留**：`linux:create:aur` 任务保留供本地测试
3. **文档说明**：

```markdown
## Arch Linux 用户

目前暂不提供官方 AUR 包。推荐使用 AppImage：

```bash
chmod +x CodeSwitch.AppImage
./CodeSwitch.AppImage
```

或手动安装到 /usr/local/bin。
```

---

## 八、中优先级修正

### 8.1 发行版依赖表核对

**nfpm.yaml 修正**：

```yaml
# 默认依赖（Ubuntu 24.04+ / Debian 12+）
depends:
  - libgtk-3-0
  - libwebkit2gtk-4.1-0

overrides:
  rpm:
    depends:
      - gtk3
      - webkit2gtk4.1

  archlinux:
    depends:
      - gtk3
      - webkit2gtk-4.1
```

**支持的发行版明确列表**：

| 发行版 | 版本 | 格式 | 状态 |
|--------|------|------|------|
| Ubuntu | 24.04 LTS | DEB | 完全支持 |
| Ubuntu | 22.04 LTS | AppImage | 仅 AppImage（WebKit 4.0） |
| Debian | 12 (Bookworm) | DEB | 完全支持 |
| Fedora | 39/40 | RPM | 完全支持 |
| Arch Linux | Rolling | AppImage | 仅 AppImage |
| Linux Mint | 22+ | DEB | 完全支持 |

### 8.2 打包格式数量修正

原文档错误描述为 "5 种格式"，实际为 **4 种**：
1. AppImage
2. DEB
3. RPM
4. AUR（暂不发布）

### 8.3 更新机制一致性

确保 Linux 更新与 Windows 保持以下一致性：

| 功能 | Windows | Linux（修正后） |
|------|---------|----------------|
| SHA256 校验 | 是 | 是 |
| 备份保留数 | 2 | 2 |
| 格式校验 | PE Header | ELF Magic |
| 日志记录 | 完整 | 完整 |
| 错误恢复 | 自动回滚 | 自动回滚 |

---

## 九、实施清单

### 9.1 文件修改清单

| 文件 | 修改类型 | 描述 |
|------|---------|------|
| `.github/workflows/release.yml` | 新增 | build-linux job + release 资产 |
| `build/linux/nfpm/nfpm.yaml` | 修改 | 挂载 postremove 脚本 |
| `build/linux/nfpm/scripts/postinstall.sh` | 重写 | 添加实际逻辑 |
| `build/linux/nfpm/scripts/postremove.sh` | 重写 | 添加卸载逻辑 |
| `services/updateservice.go` | 修改 | 增强 applyUpdateLinux |
| `main.go` | 修改 | cleanupOldFiles 平台隔离 |

### 9.2 不修改的文件

| 文件 | 原因 |
|------|------|
| `build/linux/Taskfile.yml` | 配置完整，无需修改 |
| `build/linux/desktop` | 命名已正确 |
| `build/linux/appimage/build.sh` | 功能正常 |

### 9.3 执行顺序

```
1. 修改 .github/workflows/release.yml
   ├─ 添加 build-linux job
   ├─ 修改 create-release needs
   └─ 修改 release assets 收集

2. 修改 build/linux/nfpm/nfpm.yaml
   └─ 取消 postremove 注释

3. 重写 nfpm scripts
   ├─ postinstall.sh
   └─ postremove.sh

4. 修改 services/updateservice.go
   └─ 增强 applyUpdateLinux

5. 修改 main.go
   └─ cleanupOldFiles 平台隔离

6. 本地测试
   └─ wails3 task linux:package

7. 推送 tag 触发 CI
   └─ git tag v1.2.0 && git push --tags
```

---

## 十、验收标准

### 10.1 CI 构建验收

- [ ] `build-linux` job 成功完成
- [ ] 生成 `CodeSwitch.AppImage`
- [ ] 生成 `codeswitch_*.deb`
- [ ] 生成 `codeswitch-*.rpm`
- [ ] 生成所有 `.sha256` 校验文件

### 10.2 Release 发布验收

- [ ] GitHub Release 页面包含 Linux 产物
- [ ] 下载链接可用
- [ ] SHA256 文件内容正确

### 10.3 功能验收

- [ ] AppImage 在 Ubuntu 24.04 正常运行
- [ ] DEB 包在 Debian 12 正常安装
- [ ] RPM 包在 Fedora 40 正常安装
- [ ] 自动更新检测到新版本
- [ ] 更新下载后 SHA256 校验通过
- [ ] 更新应用后程序正常重启

---

## 十一、风险与缓解

| 风险 | 可能性 | 影响 | 缓解措施 |
|------|--------|------|---------|
| ubuntu-24.04 Runner 不稳定 | 低 | 中 | 可回退到 22.04 + WebKit 4.0 |
| AppImage FUSE 依赖问题 | 中 | 低 | 文档说明 `--appimage-extract-and-run` |
| DEB/RPM 依赖冲突 | 低 | 中 | 明确支持的发行版版本 |
| 更新失败无法恢复 | 低 | 高 | 备份机制 + 日志记录 |

---

## 十二、附录

### A. 完整的 release.yml 差异

```diff
 jobs:
   build-macos:
     # ... 保持不变

   build-windows:
     # ... 保持不变

+  build-linux:
+    name: Build Linux
+    runs-on: ubuntu-24.04
+    steps:
+      # ... 见第三章

   create-release:
     name: Create Release
-    needs: [build-macos, build-windows]
+    needs: [build-macos, build-windows, build-linux]
     runs-on: ubuntu-latest
     steps:
       # ... 见第四章
```

### B. 测试命令参考

```bash
# 本地构建测试
wails3 task linux:build
wails3 task linux:create:appimage
wails3 task linux:create:deb
wails3 task linux:create:rpm

# 验证产物
file bin/CodeSwitch
file bin/CodeSwitch.AppImage
dpkg-deb -I bin/codeswitch_*.deb
rpm -qip bin/codeswitch-*.rpm

# 测试安装（DEB）
sudo dpkg -i bin/codeswitch_*.deb
sudo apt-get install -f
CodeSwitch --version

# 测试卸载
sudo dpkg -r codeswitch
```

---

**文档结束**

---

**变更记录**

| 版本 | 日期 | 作者 | 描述 |
|------|------|------|------|
| v1.0 | 2025-12-01 | Half open flowers | 初稿 |
| v2.0 | 2025-12-01 | Half open flowers | 根据审查意见修正所有阻断级和高优先级问题 |
| v2.1 | 2025-12-01 | Half open flowers | 补齐实施细节：(1) UpdateService 新增 latestUpdateInfo 字段及 SHA256 解析逻辑 (2) cleanupOldFiles 完整修改方案 (3) nfpm 脚本权限设置步骤 (4) Debian 12 依赖版本验证 |
