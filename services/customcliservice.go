package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pelletier/go-toml/v2"
)

// CustomCliTool 自定义 CLI 工具配置
type CustomCliTool struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	ConfigFiles    []ConfigFile     `json:"configFiles"`
	ProxyInjection []ProxyInjection `json:"proxyInjection,omitempty"`
}

// ConfigFile 配置文件信息
type ConfigFile struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Path      string `json:"path"`
	Format    string `json:"format"`              // json | toml | env
	IsPrimary bool   `json:"isPrimary,omitempty"`
}

// ProxyInjection 代理注入配置
type ProxyInjection struct {
	TargetFileID   string `json:"targetFileId"`
	BaseUrlField   string `json:"baseUrlField"`
	AuthTokenField string `json:"authTokenField,omitempty"`
}

// CustomCliProxyStatus 代理状态
type CustomCliProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"baseUrl"`
}

// customCliStore 存储结构
type customCliStore struct {
	Tools []CustomCliTool `json:"tools"`
}

// CustomCliService 自定义 CLI 工具服务
type CustomCliService struct {
	mu        sync.RWMutex
	relayAddr string
}

// NewCustomCliService 创建服务实例
func NewCustomCliService(relayAddr string) *CustomCliService {
	return &CustomCliService{relayAddr: relayAddr}
}

// ========== 工具 CRUD ==========

// ListTools 获取所有自定义 CLI 工具
func (s *CustomCliService) ListTools() ([]CustomCliTool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, err := s.loadStore()
	if err != nil {
		return nil, err
	}
	return store.Tools, nil
}

// GetTool 获取单个工具
func (s *CustomCliService) GetTool(id string) (*CustomCliTool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	store, err := s.loadStore()
	if err != nil {
		return nil, err
	}

	for i := range store.Tools {
		if store.Tools[i].ID == id {
			return &store.Tools[i], nil
		}
	}
	return nil, fmt.Errorf("未找到 ID 为 %s 的工具", id)
}

// CreateTool 创建新工具
func (s *CustomCliService) CreateTool(tool CustomCliTool) (*CustomCliTool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 验证必填字段
	if tool.Name == "" {
		return nil, errors.New("工具名称不能为空")
	}
	if len(tool.ConfigFiles) == 0 {
		return nil, errors.New("至少需要一个配置文件")
	}

	// 生成 ID
	tool.ID = uuid.New().String()

	// 为配置文件生成 ID（如果未设置）
	for i := range tool.ConfigFiles {
		if tool.ConfigFiles[i].ID == "" {
			tool.ConfigFiles[i].ID = fmt.Sprintf("file-%d", i+1)
		}
	}

	// 确保至少有一个主配置文件
	hasPrimary := false
	for _, f := range tool.ConfigFiles {
		if f.IsPrimary {
			hasPrimary = true
			break
		}
	}
	if !hasPrimary && len(tool.ConfigFiles) > 0 {
		tool.ConfigFiles[0].IsPrimary = true
	}

	// 加载并追加
	store, err := s.loadStore()
	if err != nil {
		store = &customCliStore{Tools: []CustomCliTool{}}
	}
	store.Tools = append(store.Tools, tool)

	if err := s.saveStore(store); err != nil {
		return nil, err
	}

	// 创建供应商目录
	if err := s.ensureProvidersDir(); err != nil {
		return nil, err
	}

	return &tool, nil
}

// UpdateTool 更新工具
func (s *CustomCliService) UpdateTool(id string, tool CustomCliTool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadStore()
	if err != nil {
		return err
	}

	found := false
	for i := range store.Tools {
		if store.Tools[i].ID == id {
			tool.ID = id // 保持 ID 不变
			store.Tools[i] = tool
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("未找到 ID 为 %s 的工具", id)
	}

	return s.saveStore(store)
}

// DeleteTool 删除工具
func (s *CustomCliService) DeleteTool(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.loadStore()
	if err != nil {
		return err
	}

	// 查找并删除
	found := false
	newTools := make([]CustomCliTool, 0, len(store.Tools))
	for _, t := range store.Tools {
		if t.ID == id {
			found = true
			continue
		}
		newTools = append(newTools, t)
	}

	if !found {
		return fmt.Errorf("未找到 ID 为 %s 的工具", id)
	}

	store.Tools = newTools
	if err := s.saveStore(store); err != nil {
		return err
	}

	// 删除对应的供应商文件
	providersPath := s.getProvidersPath(id)
	_ = os.Remove(providersPath) // 忽略错误（文件可能不存在）

	return nil
}

// ========== 代理管理 ==========

// ProxyStatus 获取代理状态
func (s *CustomCliService) ProxyStatus(toolId string) (*CustomCliProxyStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return &CustomCliProxyStatus{Enabled: false, BaseURL: s.baseURLWithToolPath(toolId)}, err
	}

	status := &CustomCliProxyStatus{
		Enabled: false,
		BaseURL: s.baseURLWithToolPath(toolId),
	}

	// 检查所有代理注入配置
	if len(tool.ProxyInjection) == 0 {
		return status, nil
	}

	allEnabled := true
	for _, injection := range tool.ProxyInjection {
		// 找到目标文件
		var targetFile *ConfigFile
		for i := range tool.ConfigFiles {
			if tool.ConfigFiles[i].ID == injection.TargetFileID {
				targetFile = &tool.ConfigFiles[i]
				break
			}
		}
		if targetFile == nil {
			allEnabled = false
			continue
		}

		// 读取并检查配置
		configPath := s.expandPath(targetFile.Path)
		content, err := os.ReadFile(configPath)
		if err != nil {
			allEnabled = false
			continue
		}

		// 检查代理字段是否已设置
		enabled, err := s.checkProxyField(content, targetFile.Format, injection.BaseUrlField, s.baseURLWithToolPath(toolId))
		if err != nil || !enabled {
			allEnabled = false
			continue
		}

		// 校验可选的鉴权字段，避免误判为已启用
		// 向后兼容：同时检查 code-switch-r（新）和 code-switch（旧）两个 token
		if injection.AuthTokenField != "" {
			authOk := false
			for _, token := range []string{"code-switch-r", "code-switch"} {
				authEnabled, err := s.checkProxyField(content, targetFile.Format, injection.AuthTokenField, token)
				if err == nil && authEnabled {
					authOk = true
					break
				}
			}
			if !authOk {
				allEnabled = false
			}
		}
	}

	status.Enabled = allEnabled
	return status, nil
}

// EnableProxy 启用代理
func (s *CustomCliService) EnableProxy(toolId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return err
	}

	if len(tool.ProxyInjection) == 0 {
		return errors.New("未配置代理注入规则")
	}

	// 对每个注入配置执行
	for _, injection := range tool.ProxyInjection {
		var targetFile *ConfigFile
		for i := range tool.ConfigFiles {
			if tool.ConfigFiles[i].ID == injection.TargetFileID {
				targetFile = &tool.ConfigFiles[i]
				break
			}
		}
		if targetFile == nil {
			return fmt.Errorf("找不到目标文件: %s", injection.TargetFileID)
		}

		configPath := s.expandPath(targetFile.Path)

		// 创建备份
		if FileExists(configPath) {
			backupPath := configPath + ".code-switch.backup"
			content, err := os.ReadFile(configPath)
			if err != nil {
				return fmt.Errorf("读取配置文件失败: %w", err)
			}
			if err := os.WriteFile(backupPath, content, 0o600); err != nil {
				return fmt.Errorf("创建备份失败: %w", err)
			}
		}

		// 写入代理字段（传递 toolId 以构建正确的代理路径）
		if err := s.injectProxyField(configPath, targetFile.Format, injection, toolId); err != nil {
			return fmt.Errorf("注入代理字段失败 (%s): %w", targetFile.Label, err)
		}
	}

	return nil
}

// DisableProxy 禁用代理
func (s *CustomCliService) DisableProxy(toolId string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return err
	}

	// 恢复所有配置文件的备份
	for _, injection := range tool.ProxyInjection {
		var targetFile *ConfigFile
		for i := range tool.ConfigFiles {
			if tool.ConfigFiles[i].ID == injection.TargetFileID {
				targetFile = &tool.ConfigFiles[i]
				break
			}
		}
		if targetFile == nil {
			continue
		}

		configPath := s.expandPath(targetFile.Path)
		backupPath := configPath + ".code-switch.backup"

		// 尝试从备份恢复
		if FileExists(backupPath) {
			if err := RestoreBackup(backupPath, configPath); err != nil {
				return fmt.Errorf("恢复备份失败 (%s): %w", targetFile.Label, err)
			}
			_ = os.Remove(backupPath)
		} else {
			// 无备份，尝试清理注入的字段
			if err := s.removeProxyField(configPath, targetFile.Format, injection); err != nil {
				// 忽略错误，可能文件不存在
				continue
			}
		}
	}

	return nil
}

// ========== 配置文件读写 ==========

// GetConfigContent 获取配置文件内容
func (s *CustomCliService) GetConfigContent(toolId, fileId string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return "", err
	}

	var targetFile *ConfigFile
	for i := range tool.ConfigFiles {
		if tool.ConfigFiles[i].ID == fileId {
			targetFile = &tool.ConfigFiles[i]
			break
		}
	}
	if targetFile == nil {
		return "", fmt.Errorf("找不到文件: %s", fileId)
	}

	configPath := s.expandPath(targetFile.Path)
	content, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("配置文件不存在: %s", configPath)
		}
		return "", err
	}

	return string(content), nil
}

// SaveConfigContent 保存配置文件内容
func (s *CustomCliService) SaveConfigContent(toolId, fileId, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return err
	}

	var targetFile *ConfigFile
	for i := range tool.ConfigFiles {
		if tool.ConfigFiles[i].ID == fileId {
			targetFile = &tool.ConfigFiles[i]
			break
		}
	}
	if targetFile == nil {
		return fmt.Errorf("找不到文件: %s", fileId)
	}

	configPath := s.expandPath(targetFile.Path)

	// 验证格式
	if err := s.validateFormat(content, targetFile.Format); err != nil {
		return fmt.Errorf("格式验证失败: %w", err)
	}

	// 创建备份
	if FileExists(configPath) {
		if _, err := CreateBackup(configPath); err != nil {
			// 备份失败不阻止保存
			fmt.Printf("创建备份失败: %v\n", err)
		}
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	// 原子写入
	return AtomicWriteText(configPath, content)
}

// GetLockedFields 获取锁定字段列表
func (s *CustomCliService) GetLockedFields(toolId string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tool, err := s.getToolLocked(toolId)
	if err != nil {
		return nil, err
	}

	var locked []string
	for _, injection := range tool.ProxyInjection {
		if injection.BaseUrlField != "" {
			locked = append(locked, injection.BaseUrlField)
		}
		if injection.AuthTokenField != "" {
			locked = append(locked, injection.AuthTokenField)
		}
	}

	return locked, nil
}

// ========== 内部方法 ==========

func (s *CustomCliService) getStorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch", "custom-cli.json")
}

func (s *CustomCliService) getProvidersDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".code-switch", "providers")
}

func (s *CustomCliService) getProvidersPath(toolId string) string {
	return filepath.Join(s.getProvidersDir(), toolId+".json")
}

func (s *CustomCliService) ensureProvidersDir() error {
	return EnsureDir(s.getProvidersDir())
}

func (s *CustomCliService) loadStore() (*customCliStore, error) {
	path := s.getStorePath()
	var store customCliStore

	if err := ReadJSONFile(path, &store); err != nil {
		if os.IsNotExist(err) {
			return &customCliStore{Tools: []CustomCliTool{}}, nil
		}
		return nil, err
	}

	return &store, nil
}

func (s *CustomCliService) saveStore(store *customCliStore) error {
	path := s.getStorePath()
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return AtomicWriteJSON(path, store)
}

func (s *CustomCliService) getToolLocked(id string) (*CustomCliTool, error) {
	store, err := s.loadStore()
	if err != nil {
		return nil, err
	}

	for i := range store.Tools {
		if store.Tools[i].ID == id {
			return &store.Tools[i], nil
		}
	}
	return nil, fmt.Errorf("未找到 ID 为 %s 的工具", id)
}

func (s *CustomCliService) baseURL() string {
	addr := strings.TrimSpace(s.relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host
}

// baseURLWithToolPath 返回包含 /custom/{toolId} 路径的完整代理 URL
// 自定义 CLI 工具的路由格式为 /custom/:toolId/v1/messages
func (s *CustomCliService) baseURLWithToolPath(toolId string) string {
	base := s.baseURL()
	// 移除尾部斜杠（如果有）
	base = strings.TrimSuffix(base, "/")
	return base + "/custom/" + toolId
}

func (s *CustomCliService) expandPath(path string) string {
	// 处理 Unix 风格路径 ~/
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	// 处理 Windows 风格路径 ~\
	if strings.HasPrefix(path, "~\\") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	// 处理单独的 ~ (表示家目录)
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	return path
}

// checkProxyField 检查代理字段是否已正确设置
func (s *CustomCliService) checkProxyField(content []byte, format, fieldPath, expectedValue string) (bool, error) {
	var data map[string]interface{}

	switch strings.ToLower(format) {
	case "json":
		if err := json.Unmarshal(content, &data); err != nil {
			return false, err
		}
	case "toml":
		if err := toml.Unmarshal(content, &data); err != nil {
			return false, err
		}
	case "env":
		envMap := parseEnvFile(string(content))
		// ENV 格式：取字段路径的最后一部分作为键名
		key := fieldPath
		if idx := strings.LastIndex(fieldPath, "."); idx >= 0 {
			key = fieldPath[idx+1:]
		}
		return envMap[key] == expectedValue, nil
	default:
		return false, fmt.Errorf("不支持的格式: %s", format)
	}

	// 检查嵌套字段
	value := getNestedValue(data, fieldPath)
	if str, ok := value.(string); ok {
		return str == expectedValue, nil
	}
	return false, nil
}

// injectProxyField 注入代理字段
// toolId 用于构建包含 /custom/{toolId} 路径的完整代理 URL
func (s *CustomCliService) injectProxyField(configPath, format string, injection ProxyInjection, toolId string) error {
	// 读取现有内容（如果存在）
	var data map[string]interface{}
	content, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if len(content) > 0 {
		switch strings.ToLower(format) {
		case "json":
			if err := json.Unmarshal(content, &data); err != nil {
				data = make(map[string]interface{})
			}
		case "toml":
			if err := toml.Unmarshal(content, &data); err != nil {
				data = make(map[string]interface{})
			}
		case "env":
			// ENV 格式特殊处理
			return s.injectEnvField(configPath, content, injection, toolId)
		}
	} else {
		data = make(map[string]interface{})
	}

	// 设置代理字段（使用包含 toolId 的完整路径）
	setNestedValue(data, injection.BaseUrlField, s.baseURLWithToolPath(toolId))
	if injection.AuthTokenField != "" {
		setNestedValue(data, injection.AuthTokenField, "code-switch-r")
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	// 写回文件
	switch strings.ToLower(format) {
	case "json":
		return AtomicWriteJSON(configPath, data)
	case "toml":
		tomlData, err := toml.Marshal(data)
		if err != nil {
			return err
		}
		return AtomicWriteBytes(configPath, tomlData)
	}

	return nil
}

// injectEnvField 注入 ENV 格式的代理字段
// toolId 用于构建包含 /custom/{toolId} 路径的完整代理 URL
func (s *CustomCliService) injectEnvField(configPath string, content []byte, injection ProxyInjection, toolId string) error {
	envMap := parseEnvFile(string(content))

	// ENV 格式：取字段路径的最后一部分作为键名
	baseUrlKey := injection.BaseUrlField
	if idx := strings.LastIndex(baseUrlKey, "."); idx >= 0 {
		baseUrlKey = baseUrlKey[idx+1:]
	}
	envMap[baseUrlKey] = s.baseURLWithToolPath(toolId)

	if injection.AuthTokenField != "" {
		authKey := injection.AuthTokenField
		if idx := strings.LastIndex(authKey, "."); idx >= 0 {
			authKey = authKey[idx+1:]
		}
		envMap[authKey] = "code-switch-r"
	}

	// 确保目录存在
	if err := EnsureDir(filepath.Dir(configPath)); err != nil {
		return err
	}

	return AtomicWriteText(configPath, serializeEnvFile(envMap))
}

// removeProxyField 移除代理字段
func (s *CustomCliService) removeProxyField(configPath, format string, injection ProxyInjection) error {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	var data map[string]interface{}

	switch strings.ToLower(format) {
	case "json":
		if err := json.Unmarshal(content, &data); err != nil {
			return err
		}
		deleteNestedValue(data, injection.BaseUrlField)
		if injection.AuthTokenField != "" {
			deleteNestedValue(data, injection.AuthTokenField)
		}
		return AtomicWriteJSON(configPath, data)

	case "toml":
		if err := toml.Unmarshal(content, &data); err != nil {
			return err
		}
		deleteNestedValue(data, injection.BaseUrlField)
		if injection.AuthTokenField != "" {
			deleteNestedValue(data, injection.AuthTokenField)
		}
		tomlData, err := toml.Marshal(data)
		if err != nil {
			return err
		}
		return AtomicWriteBytes(configPath, tomlData)

	case "env":
		envMap := parseEnvFile(string(content))
		baseUrlKey := injection.BaseUrlField
		if idx := strings.LastIndex(baseUrlKey, "."); idx >= 0 {
			baseUrlKey = baseUrlKey[idx+1:]
		}
		delete(envMap, baseUrlKey)
		if injection.AuthTokenField != "" {
			authKey := injection.AuthTokenField
			if idx := strings.LastIndex(authKey, "."); idx >= 0 {
				authKey = authKey[idx+1:]
			}
			delete(envMap, authKey)
		}
		return AtomicWriteText(configPath, serializeEnvFile(envMap))
	}

	return nil
}

// validateFormat 验证内容格式
func (s *CustomCliService) validateFormat(content, format string) error {
	switch strings.ToLower(format) {
	case "json":
		var data interface{}
		return json.Unmarshal([]byte(content), &data)
	case "toml":
		var data interface{}
		return toml.Unmarshal([]byte(content), &data)
	case "env":
		// ENV 格式不做严格验证
		return nil
	default:
		return fmt.Errorf("不支持的格式: %s", format)
	}
}

// ========== 嵌套字段操作辅助函数 ==========

// getNestedValue 获取嵌套字段值
func getNestedValue(data map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := interface{}(data)

	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			current = m[part]
		} else {
			return nil
		}
	}

	return current
}

// setNestedValue 设置嵌套字段值
func setNestedValue(data map[string]interface{}, path string, value interface{}) {
	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			// 最后一层，设置值
			current[part] = value
		} else {
			// 中间层，确保存在 map
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			} else {
				next := make(map[string]interface{})
				current[part] = next
				current = next
			}
		}
	}
}

// deleteNestedValue 删除嵌套字段
func deleteNestedValue(data map[string]interface{}, path string) {
	parts := strings.Split(path, ".")
	current := data

	for i, part := range parts {
		if i == len(parts)-1 {
			delete(current, part)
		} else {
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			} else {
				return
			}
		}
	}
}
