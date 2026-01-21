package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Prompt 自定义提示词
type Prompt struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Content     string  `json:"content"`
	Description *string `json:"description,omitempty"`
	Enabled     bool    `json:"enabled"`
	CreatedAt   *int64  `json:"createdAt,omitempty"`
	UpdatedAt   *int64  `json:"updatedAt,omitempty"`
}

// PromptConfig 提示词配置（按平台分组）
type PromptConfig struct {
	Claude map[string]Prompt `json:"claude"`
	Codex  map[string]Prompt `json:"codex"`
	Gemini map[string]Prompt `json:"gemini"`
}

// PromptService 提示词管理服务
type PromptService struct {
	mu            sync.Mutex
	config        PromptConfig
	lastWriteTime map[string]time.Time // 记录应用最后写入时间，用于回环检测
}

// NewPromptService 创建提示词服务
func NewPromptService() *PromptService {
	svc := &PromptService{
		config: PromptConfig{
			Claude: make(map[string]Prompt),
			Codex:  make(map[string]Prompt),
			Gemini: make(map[string]Prompt),
		},
		lastWriteTime: make(map[string]time.Time),
	}
	_ = svc.load()
	return svc
}

// Start Wails生命周期方法
func (s *PromptService) Start() error {
	return nil
}

// Stop Wails生命周期方法
func (s *PromptService) Stop() error {
	return nil
}

// GetPrompts 获取指定平台的所有提示词
func (s *PromptService) GetPrompts(platform string) (map[string]Prompt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 检测并同步外部修改（使用只读路径函数，避免创建目录副作用）
	filePath, err := s.getPromptFilePathReadOnly(platform)
	if err != nil {
		return nil, err
	}

	externallyModified, fileModTime, err := s.isExternallyModified(filePath)
	if err != nil {
		return nil, err
	}

	if externallyModified {
		if err := s.syncFromFile(platform, filePath, fileModTime); err != nil {
			return nil, err
		}
	}

	switch platform {
	case "claude":
		return s.deepCopyMap(s.config.Claude), nil
	case "codex":
		return s.deepCopyMap(s.config.Codex), nil
	case "gemini":
		return s.deepCopyMap(s.config.Gemini), nil
	default:
		return nil, fmt.Errorf("不支持的平台: %s", platform)
	}
}

// UpsertPrompt 添加或更新提示词
func (s *PromptService) UpsertPrompt(platform, id string, prompt Prompt) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prompts, err := s.getPromptsForPlatform(platform)
	if err != nil {
		return err
	}

	// 设置 ID
	if prompt.ID == "" {
		prompt.ID = id
	}

	// 设置时间戳
	now := time.Now().Unix()
	if prompt.CreatedAt == nil {
		prompt.CreatedAt = &now
	}
	prompt.UpdatedAt = &now

	// 保存到内存
	(*prompts)[id] = prompt

	// 持久化
	if err := s.save(); err != nil {
		return err
	}

	// 如果启用，写入目标文件
	if prompt.Enabled {
		filePath, err := s.getPromptFilePath(platform)
		if err != nil {
			return err
		}
		return s.writePromptFile(filePath, prompt.Content)
	}

	return nil
}

// DeletePrompt 删除提示词
func (s *PromptService) DeletePrompt(platform, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prompts, err := s.getPromptsForPlatform(platform)
	if err != nil {
		return err
	}

	// 检查是否已启用
	if prompt, exists := (*prompts)[id]; exists && prompt.Enabled {
		return fmt.Errorf("无法删除已启用的提示词")
	}

	// 删除
	delete(*prompts, id)

	return s.save()
}

// EnablePrompt 启用指定提示词（会禁用其他所有提示词）
func (s *PromptService) EnablePrompt(platform, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	prompts, err := s.getPromptsForPlatform(platform)
	if err != nil {
		return err
	}

	// 检查目标提示词是否存在
	targetPrompt, exists := (*prompts)[id]
	if !exists {
		return fmt.Errorf("提示词 %s 不存在", id)
	}

	// 获取提示词文件路径
	filePath, err := s.getPromptFilePath(platform)
	if err != nil {
		return err
	}

	// 备份当前文件内容（如果存在）
	if err := s.backupCurrentPrompt(platform, filePath, prompts); err != nil {
		return fmt.Errorf("备份当前提示词失败: %w", err)
	}

	// 禁用所有提示词
	for key := range *prompts {
		p := (*prompts)[key]
		p.Enabled = false
		(*prompts)[key] = p
	}

	// 启用目标提示词
	targetPrompt.Enabled = true
	now := time.Now().Unix()
	targetPrompt.UpdatedAt = &now
	(*prompts)[id] = targetPrompt

	// 持久化配置
	if err := s.save(); err != nil {
		return err
	}

	// 写入提示词文件
	return s.writePromptFile(filePath, targetPrompt.Content)
}

// ImportFromFile 从现有文件导入提示词
func (s *PromptService) ImportFromFile(platform string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	filePath, err := s.getPromptFilePath(platform)
	if err != nil {
		return "", err
	}

	// 读取文件内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取提示词文件失败: %w", err)
	}

	// 生成ID
	now := time.Now().Unix()
	id := fmt.Sprintf("imported-%d", now)

	// 创建提示词
	desc := "从现有配置文件导入"
	prompt := Prompt{
		ID:          id,
		Name:        fmt.Sprintf("导入的提示词 %s", time.Now().Format("2006-01-02 15:04")),
		Content:     string(content),
		Description: &desc,
		Enabled:     false,
		CreatedAt:   &now,
		UpdatedAt:   &now,
	}

	// 保存
	prompts, err := s.getPromptsForPlatform(platform)
	if err != nil {
		return "", err
	}
	(*prompts)[id] = prompt

	if err := s.save(); err != nil {
		return "", err
	}

	return id, nil
}

// GetCurrentFileContent 获取当前提示词文件内容
func (s *PromptService) GetCurrentFileContent(platform string) (*string, error) {
	filePath, err := s.getPromptFilePathReadOnly(platform)
	if err != nil {
		return nil, err
	}

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取提示词文件失败: %w", err)
	}

	result := string(content)
	return &result, nil
}

// backupCurrentPrompt 备份当前提示词文件内容
func (s *PromptService) backupCurrentPrompt(platform, filePath string, prompts *map[string]Prompt) error {
	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil // 文件不存在，无需备份
	}

	// 读取当前文件内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	contentStr := string(content)
	if len(contentStr) == 0 {
		return nil // 空文件，无需备份
	}

	// 检查是否已有已启用的提示词
	var enabledPrompt *Prompt
	var enabledID string
	for id, p := range *prompts {
		if p.Enabled {
			enabledPrompt = &p
			enabledID = id
			break
		}
	}

	now := time.Now().Unix()

	if enabledPrompt != nil {
		// 回填到已启用的提示词
		enabledPrompt.Content = contentStr
		enabledPrompt.UpdatedAt = &now
		(*prompts)[enabledID] = *enabledPrompt
	} else {
		// 检查是否已存在相同内容的提示词
		contentExists := false
		for _, p := range *prompts {
			if p.Content == contentStr {
				contentExists = true
				break
			}
		}

		if !contentExists {
			// 创建备份
			backupID := fmt.Sprintf("backup-%d", now)
			desc := "自动备份的原始提示词"
			backup := Prompt{
				ID:          backupID,
				Name:        fmt.Sprintf("原始提示词 %s", time.Now().Format("2006-01-02 15:04")),
				Content:     contentStr,
				Description: &desc,
				Enabled:     false,
				CreatedAt:   &now,
				UpdatedAt:   &now,
			}
			(*prompts)[backupID] = backup
		}
	}

	return nil
}

// getPromptsForPlatform 获取指定平台的提示词映射引用
func (s *PromptService) getPromptsForPlatform(platform string) (*map[string]Prompt, error) {
	switch platform {
	case "claude":
		return &s.config.Claude, nil
	case "codex":
		return &s.config.Codex, nil
	case "gemini":
		return &s.config.Gemini, nil
	default:
		return nil, fmt.Errorf("不支持的平台: %s", platform)
	}
}

// getPromptFilePathReadOnly 获取提示词文件路径（只读，不创建目录）
func (s *PromptService) getPromptFilePathReadOnly(platform string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取用户主目录: %w", err)
	}

	var dir, filename string
	switch platform {
	case "claude":
		dir = filepath.Join(home, ".claude")
		filename = "CLAUDE.md"
	case "codex":
		dir = filepath.Join(home, ".codex")
		filename = "AGENTS.md"
	case "gemini":
		dir = filepath.Join(home, ".gemini")
		filename = "GEMINI.md"
	default:
		return "", fmt.Errorf("不支持的平台: %s", platform)
	}

	return filepath.Join(dir, filename), nil
}

// getPromptFilePath 获取提示词文件路径（会创建目录）
func (s *PromptService) getPromptFilePath(platform string) (string, error) {
	filePath, err := s.getPromptFilePathReadOnly(platform)
	if err != nil {
		return "", err
	}

	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("创建目录失败: %w", err)
	}

	return filePath, nil
}

// writePromptFile 原子写入提示词文件
func (s *PromptService) writePromptFile(path, content string) error {
	// 确保目录存在
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	// 原子写入
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // 清理临时文件
		return fmt.Errorf("重命名文件失败: %w", err)
	}

	// 记录写入时间，用于回环检测
	if info, err := os.Stat(path); err == nil {
		s.lastWriteTime[path] = info.ModTime()
	} else {
		s.lastWriteTime[path] = time.Now()
	}

	return nil
}

// isExternallyModified 检测文件是否被外部修改
func (s *PromptService) isExternallyModified(filePath string) (bool, time.Time, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, time.Time{}, nil
		}
		return false, time.Time{}, err
	}

	modTime := info.ModTime()
	last, ok := s.lastWriteTime[filePath]
	if !ok {
		// 首次访问，认为是外部修改（需要同步）
		return true, modTime, nil
	}

	// 使用 100ms 容差处理时间精度问题
	const tolerance = 100 * time.Millisecond
	if modTime.After(last.Add(tolerance)) || modTime.Before(last.Add(-tolerance)) {
		return true, modTime, nil
	}

	return false, modTime, nil
}

// syncFromFile 从文件同步到已启用的提示词
func (s *PromptService) syncFromFile(platform, filePath string, fileModTime time.Time) error {
	prompts, err := s.getPromptsForPlatform(platform)
	if err != nil {
		return err
	}

	// 查找已启用的提示词
	var enabledID string
	var enabledPrompt Prompt
	found := false
	for id, p := range *prompts {
		if p.Enabled {
			enabledID = id
			enabledPrompt = p
			found = true
			break
		}
	}

	// 没有启用的提示词，不做处理
	if !found {
		// 更新 lastWriteTime 避免重复检测
		if !fileModTime.IsZero() {
			s.lastWriteTime[filePath] = fileModTime
		}
		return nil
	}

	// 读取文件内容
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取提示词文件失败: %w", err)
	}

	contentStr := string(content)

	// 内容相同，无需同步
	if enabledPrompt.Content == contentStr {
		if !fileModTime.IsZero() {
			s.lastWriteTime[filePath] = fileModTime
		}
		return nil
	}

	// 回填到已启用的提示词
	now := time.Now().Unix()
	enabledPrompt.Content = contentStr
	enabledPrompt.UpdatedAt = &now
	(*prompts)[enabledID] = enabledPrompt

	// 持久化到 prompts.json
	if err := s.save(); err != nil {
		return err
	}

	// 更新 lastWriteTime
	if !fileModTime.IsZero() {
		s.lastWriteTime[filePath] = fileModTime
	}

	return nil
}

// load 加载配置
func (s *PromptService) load() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(home, ".code-switch", "prompts.json")

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在，使用默认配置
		}
		return err
	}

	return json.Unmarshal(data, &s.config)
}

// save 保存配置
func (s *PromptService) save() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "prompts.json")

	data, err := json.MarshalIndent(s.config, "", "  ")
	if err != nil {
		return err
	}

	// 原子写入
	tmpPath := configPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, configPath)
}

// deepCopyMap 深拷贝提示词映射
func (s *PromptService) deepCopyMap(src map[string]Prompt) map[string]Prompt {
	result := make(map[string]Prompt, len(src))
	for k, v := range src {
		result[k] = v
	}
	return result
}
