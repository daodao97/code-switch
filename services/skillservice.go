package services

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	skillStoreDir  = ".code-switch"
	skillStoreFile = "skill.json"

	// 平台常量
	skillPlatformClaude = "claude"
	skillPlatformCodex  = "codex"

	// 安装位置常量
	skillLocationUser    = "user"
	skillLocationProject = "project"
)

var (
	defaultRepoBranches = []string{"main", "master"}
	defaultSkillRepos   = []skillRepoConfig{
		{Owner: "ComposioHQ", Name: "awesome-claude-skills", Branch: "main", Enabled: true},
		{Owner: "anthropics", Name: "skills", Branch: "main", Enabled: true},
	}
)

type Skill struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Directory   string `json:"directory"`
	ReadmeURL   string `json:"readme_url"`
	Installed   bool   `json:"installed"`

	// 新增字段
	Enabled         bool   `json:"enabled"`                     // 是否启用（从 SKILL.md 读取）
	LicenseFile     string `json:"license_file,omitempty"`      // 许可证文件路径
	Platform        string `json:"platform,omitempty"`          // "claude" | "codex"
	InstallLocation string `json:"install_location,omitempty"`  // "user" | "project"

	// 仓库字段
	RepoOwner  string `json:"repo_owner,omitempty"`
	RepoName   string `json:"repo_name,omitempty"`
	RepoBranch string `json:"repo_branch,omitempty"`
}

type skillMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// skillMetadataExtended 扩展的元数据结构（包含 enabled 状态相关字段）
type skillMetadataExtended struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
	UserInvocable          *bool  `yaml:"user-invocable"`
}

type skillStore struct {
	Skills map[string]skillState `json:"skills"`
	Repos  []skillRepoConfig     `json:"repos"`
}

type skillState struct {
	Installed   bool      `json:"installed"`
	InstalledAt time.Time `json:"installed_at,omitempty"`
}

type skillRepoConfig struct {
	Owner   string `json:"owner"`
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	Enabled bool   `json:"enabled"`
}

type installRequest struct {
	Directory string `json:"directory"`
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
	Branch    string `json:"repo_branch"`
	Platform  string `json:"platform"`  // "claude" | "codex"
	Location  string `json:"location"`  // "user" | "project"
}

type SkillService struct {
	httpClient *http.Client
	storePath  string
	installDir string
	mu         sync.Mutex
}

func NewSkillService() *SkillService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return &SkillService{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		storePath:  filepath.Join(home, skillStoreDir, skillStoreFile),
		installDir: filepath.Join(home, ".claude", "skills"),
	}
}

// getInstallPath 根据平台和位置返回 skills 目录路径
// platform: "claude" | "codex"
// location: "user" | "project"
func (ss *SkillService) getInstallPath(platform, location string) (string, error) {
	var basePath string

	switch location {
	case skillLocationProject:
		// 项目级: 使用当前工作目录
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("获取工作目录失败: %w", err)
		}
		basePath = cwd
	case skillLocationUser:
		fallthrough
	default:
		// 用户级: 使用 home 目录
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("获取用户目录失败: %w", err)
		}
		basePath = home
	}

	var configDir string
	switch platform {
	case skillPlatformCodex:
		configDir = ".codex"
	case skillPlatformClaude:
		fallthrough
	default:
		configDir = ".claude"
	}

	return filepath.Join(basePath, configDir, "skills"), nil
}

// ListSkillsForPlatform 列出指定平台的技能（用户级 + 项目级）
func (ss *SkillService) ListSkillsForPlatform(platform string) ([]Skill, error) {
	if platform == "" {
		platform = skillPlatformClaude
	}

	var allSkills []Skill

	// 扫描用户级目录
	userPath, err := ss.getInstallPath(platform, skillLocationUser)
	if err == nil {
		userSkills := ss.scanSkillsDirectory(userPath, platform, skillLocationUser)
		allSkills = append(allSkills, userSkills...)
	}

	// 扫描项目级目录
	projectPath, err := ss.getInstallPath(platform, skillLocationProject)
	if err == nil {
		projectSkills := ss.scanSkillsDirectory(projectPath, platform, skillLocationProject)
		allSkills = append(allSkills, projectSkills...)
	}

	// 按名称排序
	sort.SliceStable(allSkills, func(i, j int) bool {
		return strings.ToLower(allSkills[i].Name) < strings.ToLower(allSkills[j].Name)
	})

	return allSkills, nil
}

// scanSkillsDirectory 扫描目录中的技能
func (ss *SkillService) scanSkillsDirectory(dir, platform, location string) []Skill {
	var skills []Skill

	entries, err := os.ReadDir(dir)
	if err != nil {
		return skills
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(dir, entry.Name())
		skillMDPath := filepath.Join(skillPath, "SKILL.md")

		// 检查 SKILL.md 是否存在
		if _, err := os.Stat(skillMDPath); err != nil {
			continue
		}

		// 读取元数据
		meta, enabled, err := ss.readSkillMetadataExtended(skillPath)
		if err != nil {
			continue
		}

		name := strings.TrimSpace(meta.Name)
		if name == "" {
			name = entry.Name()
		}

		// 检查 LICENSE 文件
		licenseFile := ""
		for _, lf := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md"} {
			if _, err := os.Stat(filepath.Join(skillPath, lf)); err == nil {
				licenseFile = lf
				break
			}
		}

		skill := Skill{
			Key:             fmt.Sprintf("%s:%s:%s", platform, location, entry.Name()),
			Name:            name,
			Description:     strings.TrimSpace(meta.Description),
			Directory:       entry.Name(),
			Installed:       true,
			Enabled:         enabled,
			LicenseFile:     licenseFile,
			Platform:        platform,
			InstallLocation: location,
		}

		skills = append(skills, skill)
	}

	return skills
}

// readSkillMetadataExtended 读取技能元数据（包括 enabled 状态）
func (ss *SkillService) readSkillMetadataExtended(dir string) (skillMetadataExtended, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return skillMetadataExtended{}, false, err
	}

	meta, err := parseSkillMetadataExtended(string(data))
	if err != nil {
		return skillMetadataExtended{}, false, err
	}

	// enabled = NOT disable-model-invocation
	enabled := !meta.DisableModelInvocation

	return meta, enabled, nil
}

// parseSkillMetadataExtended 解析扩展元数据
func parseSkillMetadataExtended(content string) (skillMetadataExtended, error) {
	var meta skillMetadataExtended
	content = strings.TrimLeft(content, "\ufeff")

	// 使用 splitFrontMatter 替代 strings.SplitN，避免 YAML 值中的 --- 被误判
	_, fmLines, _, err := splitFrontMatter(content)
	if err != nil {
		return meta, errors.New("SKILL.md 缺少 front matter")
	}

	frontMatter := strings.Join(fmLines, "\n")
	if err := yaml.Unmarshal([]byte(frontMatter), &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

// ListSkills aggregates skills from configured repositories and the local install directory.
func (ss *SkillService) ListSkills() ([]Skill, error) {
	store, err := ss.loadStore()
	if err != nil {
		return nil, err
	}

	skillMap := make(map[string]Skill)
	for _, repo := range store.Repos {
		if !repo.Enabled {
			continue
		}
		repoDir, branch, cleanup, err := ss.prepareRepoSnapshot(repo)
		if err != nil {
			log.Printf("skill repo fetch failed for %s/%s: %v", repo.Owner, repo.Name, err)
			continue
		}
		entries, err := os.ReadDir(repoDir)
		if err != nil {
			cleanup()
			log.Printf("skill repo read failed for %s/%s: %v", repo.Owner, repo.Name, err)
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dirKey := normalizeDirectoryKey(entry.Name())
			if _, exists := skillMap[dirKey]; exists {
				continue
			}
			skillPath := filepath.Join(repoDir, entry.Name())
			meta, err := readSkillMetadata(skillPath)
			if err != nil {
				continue
			}
			name := strings.TrimSpace(meta.Name)
			if name == "" {
				name = entry.Name()
			}
			key := buildSkillKey(repo.Owner, repo.Name, entry.Name())
			skillMap[dirKey] = Skill{
				Key:         key,
				Name:        name,
				Description: strings.TrimSpace(meta.Description),
				Directory:   entry.Name(),
				ReadmeURL:   buildRepoURL(repo, branch, entry.Name()),
				Installed:   ss.isInstalled(entry.Name()),
				RepoOwner:   repo.Owner,
				RepoName:    repo.Name,
				RepoBranch:  branch,
			}
		}
		cleanup()
	}

	ss.mergeLocalSkills(skillMap)
	skills := make([]Skill, 0, len(skillMap))
	for _, skill := range skillMap {
		skills = append(skills, skill)
	}
	sort.SliceStable(skills, func(i, j int) bool {
		li := strings.ToLower(skills[i].Name)
		lj := strings.ToLower(skills[j].Name)
		if li == lj {
			return strings.ToLower(skills[i].Directory) < strings.ToLower(skills[j].Directory)
		}
		return li < lj
	})
	return skills, nil
}

// InstallSkill installs a skill directory from the configured repositories.
// 支持 platform 和 location 参数，用于指定安装的平台和位置
func (ss *SkillService) InstallSkill(req installRequest) error {
	req.Directory = strings.TrimSpace(req.Directory)
	if req.Directory == "" {
		return errors.New("skill directory 不能为空")
	}

	// 设置默认值
	if req.Platform == "" {
		req.Platform = skillPlatformClaude
	}
	if req.Location == "" {
		req.Location = skillLocationUser
	}

	store, err := ss.loadStore()
	if err != nil {
		return err
	}
	repos := ss.resolveReposForInstall(req, store.Repos)
	if len(repos) == 0 {
		return errors.New("未找到可用的技能仓库")
	}

	var lastErr error
	for _, repo := range repos {
		repoDir, _, cleanup, err := ss.prepareRepoSnapshot(repo)
		if err != nil {
			lastErr = err
			continue
		}
		skillPath := filepath.Join(repoDir, req.Directory)
		info, err := os.Stat(skillPath)
		if err != nil || !info.IsDir() {
			cleanup()
			lastErr = fmt.Errorf("仓库 %s/%s 中未找到 %s", repo.Owner, repo.Name, req.Directory)
			continue
		}
		if err := ss.installFromPathEx(req.Directory, skillPath, req.Platform, req.Location); err != nil {
			cleanup()
			lastErr = err
			continue
		}
		cleanup()
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("skill %s 未找到", req.Directory)
	}
	return lastErr
}

func (ss *SkillService) installFromPath(directory, source string) error {
	return ss.installFromPathEx(directory, source, skillPlatformClaude, skillLocationUser)
}

// installFromPathEx 安装技能到指定平台和位置
func (ss *SkillService) installFromPathEx(directory, source, platform, location string) error {
	if _, err := os.Stat(filepath.Join(source, "SKILL.md")); err != nil {
		return fmt.Errorf("%s 缺少 SKILL.md", directory)
	}

	// 获取安装路径
	installPath, err := ss.getInstallPath(platform, location)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(installPath, 0o755); err != nil {
		return err
	}
	target := filepath.Join(installPath, directory)
	if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := copyDirectory(source, target); err != nil {
		return err
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	store, err := ss.loadStoreLocked()
	if err != nil {
		return err
	}
	if store.Skills == nil {
		store.Skills = make(map[string]skillState)
	}
	store.Skills[directory] = skillState{Installed: true, InstalledAt: time.Now()}
	return ss.saveStoreLocked(store)
}

func (ss *SkillService) UninstallSkill(directory string) error {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return errors.New("skill directory 不能为空")
	}
	target := filepath.Join(ss.installDir, directory)
	if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	store, err := ss.loadStoreLocked()
	if err != nil {
		return err
	}
	if store.Skills == nil {
		store.Skills = make(map[string]skillState)
	}
	delete(store.Skills, directory)
	return ss.saveStoreLocked(store)
}

// UninstallSkillEx 卸载技能（支持多平台多位置）
func (ss *SkillService) UninstallSkillEx(directory, platform, location string) error {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return errors.New("skill directory 不能为空")
	}

	// 默认值
	if platform == "" {
		platform = skillPlatformClaude
	}
	if location == "" {
		location = skillLocationUser
	}

	installPath, err := ss.getInstallPath(platform, location)
	if err != nil {
		return err
	}

	target := filepath.Join(installPath, directory)
	if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
		return err
	}

	// 更新 store
	ss.mu.Lock()
	defer ss.mu.Unlock()
	store, err := ss.loadStoreLocked()
	if err != nil {
		return err
	}
	if store.Skills == nil {
		store.Skills = make(map[string]skillState)
	}
	delete(store.Skills, directory)
	return ss.saveStoreLocked(store)
}

// ToggleSkill 切换技能的启用状态
// 通过修改 SKILL.md 的 disable-model-invocation 字段实现
func (ss *SkillService) ToggleSkill(directory, platform, location string, enabled bool) error {
	if directory == "" {
		return errors.New("skill directory 不能为空")
	}

	installPath, err := ss.getInstallPath(platform, location)
	if err != nil {
		return err
	}

	skillMDPath := filepath.Join(installPath, directory, "SKILL.md")

	// 读取文件
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return fmt.Errorf("读取 SKILL.md 失败: %w", err)
	}

	// 使用最小文本补丁修改
	newContent, changed, err := patchSkillFrontMatterBool(
		string(data),
		"disable-model-invocation",
		!enabled, // enabled=true → disable-model-invocation=false
	)
	if err != nil {
		return fmt.Errorf("修改 SKILL.md 失败: %w", err)
	}

	if !changed {
		return nil // 无需修改
	}

	// 原子写入
	return AtomicWriteBytes(skillMDPath, []byte(newContent))
}

// splitFrontMatter 使用行首匹配 ^---\s*$ 来分割 front matter
// 返回 (prefix, frontMatterLines, body, error)
// prefix: 开始 --- 之前的行
// frontMatterLines: front matter 内容（不含边界行）
// body: 结束 --- 之后的所有内容
func splitFrontMatter(content string) (prefix string, fmLines []string, body string, err error) {
	// 统一行尾为 \n 进行处理
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	startIdx := -1
	endIdx := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			if startIdx == -1 {
				startIdx = i
			} else {
				endIdx = i
				break
			}
		}
	}

	if startIdx == -1 || endIdx == -1 {
		return "", nil, "", errors.New("无法解析 front matter：未找到有效的 --- 边界")
	}

	// prefix: lines[0:startIdx]
	if startIdx > 0 {
		prefix = strings.Join(lines[:startIdx], "\n") + "\n"
	}

	// fmLines: lines[startIdx+1:endIdx]
	fmLines = lines[startIdx+1 : endIdx]

	// body: lines[endIdx+1:]
	if endIdx+1 < len(lines) {
		body = strings.Join(lines[endIdx+1:], "\n")
	}

	return prefix, fmLines, body, nil
}

// patchSkillFrontMatterBool 最小化修改 SKILL.md 的 front matter 中的布尔字段
// 保留原有格式、注释和字段顺序
func patchSkillFrontMatterBool(markdown, key string, desired bool) (string, bool, error) {
	// 1. 保留 BOM
	hasBOM := false
	if strings.HasPrefix(markdown, "\ufeff") {
		hasBOM = true
		markdown = strings.TrimPrefix(markdown, "\ufeff")
	}

	// 2. 检测行尾风格
	hasCRLF := strings.Contains(markdown, "\r\n")

	// 3. 分割 front matter（使用行首匹配，避免内容中的 --- 被误判）
	prefix, lines, body, err := splitFrontMatter(markdown)
	if err != nil {
		return "", false, err
	}

	// 4. 按行处理 front matter
	keyFound := false
	modified := false
	desiredStr := "false"
	if desired {
		desiredStr = "true"
	}

	for i, line := range lines {
		// 移除可能的 \r
		cleanLine := strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(cleanLine)

		// 检查是否匹配目标 key
		if strings.HasPrefix(trimmed, key+":") {
			keyFound = true

			// 提取当前值
			colonIdx := strings.Index(trimmed, ":")
			valuePart := strings.TrimSpace(trimmed[colonIdx+1:])

			// 处理可能的行内注释
			comment := ""
			hashIdx := strings.Index(valuePart, "#")
			if hashIdx != -1 {
				comment = valuePart[hashIdx:]
				valuePart = strings.TrimSpace(valuePart[:hashIdx])
			}

			// 检查是否需要修改
			currentBool := strings.ToLower(valuePart) == "true"
			if currentBool == desired {
				continue // 值已经正确，无需修改
			}

			// 构建新行（保留原有缩进）
			indent := ""
			for _, ch := range cleanLine {
				if ch == ' ' || ch == '\t' {
					indent += string(ch)
				} else {
					break
				}
			}

			newLine := indent + key + ": " + desiredStr
			if comment != "" {
				newLine += " " + comment
			}

			lines[i] = newLine
			modified = true
		}
	}

	// 5. 如果 key 不存在，在 front matter 末尾插入
	if !keyFound {
		insertLine := key + ": " + desiredStr
		// 在最后一行（通常是空行）之前插入
		insertIdx := len(lines) - 1
		for insertIdx > 0 && strings.TrimSpace(lines[insertIdx]) == "" {
			insertIdx--
		}
		insertIdx++

		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:insertIdx]...)
		newLines = append(newLines, insertLine)
		newLines = append(newLines, lines[insertIdx:]...)
		lines = newLines
		modified = true
	}

	// 6. 重建文档
	newFrontMatter := strings.Join(lines, "\n")
	result := prefix + "---\n" + newFrontMatter + "\n---\n" + body

	// 7. 恢复 CRLF（如果原文使用）
	if hasCRLF {
		// 先统一为 LF，再替换为 CRLF
		result = strings.ReplaceAll(result, "\r\n", "\n")
		result = strings.ReplaceAll(result, "\n", "\r\n")
	}

	// 8. 恢复 BOM
	if hasBOM {
		result = "\ufeff" + result
	}

	return result, modified, nil
}

// GetSkillContent 获取技能的 SKILL.md 内容
func (ss *SkillService) GetSkillContent(directory, platform, location string) (string, error) {
	if directory == "" {
		return "", errors.New("skill directory 不能为空")
	}

	installPath, err := ss.getInstallPath(platform, location)
	if err != nil {
		return "", err
	}

	skillMDPath := filepath.Join(installPath, directory, "SKILL.md")
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return "", fmt.Errorf("读取 SKILL.md 失败: %w", err)
	}

	return string(data), nil
}

// SaveSkillContent 保存技能的 SKILL.md 内容
func (ss *SkillService) SaveSkillContent(directory, platform, location, content string) error {
	if directory == "" {
		return errors.New("skill directory 不能为空")
	}

	installPath, err := ss.getInstallPath(platform, location)
	if err != nil {
		return err
	}

	skillMDPath := filepath.Join(installPath, directory, "SKILL.md")

	// 原子写入
	return AtomicWriteBytes(skillMDPath, []byte(content))
}

// OpenSkillFolder 打开技能目录
func (ss *SkillService) OpenSkillFolder(platform, location string) error {
	installPath, err := ss.getInstallPath(platform, location)
	if err != nil {
		return err
	}

	// 确保目录存在
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		return err
	}

	return OpenInExplorer(installPath)
}

// Repository management ----------------------------------------------------

func (ss *SkillService) ListRepos() ([]skillRepoConfig, error) {
	store, err := ss.loadStore()
	if err != nil {
		return nil, err
	}
	return cloneRepoConfigs(store.Repos), nil
}

func (ss *SkillService) AddRepo(repo skillRepoConfig) ([]skillRepoConfig, error) {
	repo = normalizeRepoConfig(repo)
	if err := validateRepoConfig(repo); err != nil {
		return nil, err
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	store, err := ss.loadStoreLocked()
	if err != nil {
		return nil, err
	}
	replaced := false
	for i := range store.Repos {
		if equalRepo(store.Repos[i], repo) {
			store.Repos[i] = repo
			replaced = true
			break
		}
	}
	if !replaced {
		store.Repos = append(store.Repos, repo)
	}
	if err := ss.saveStoreLocked(store); err != nil {
		return nil, err
	}
	return cloneRepoConfigs(store.Repos), nil
}

func (ss *SkillService) RemoveRepo(owner, name string) ([]skillRepoConfig, error) {
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	if owner == "" || name == "" {
		return nil, errors.New("owner/name 不能为空")
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	store, err := ss.loadStoreLocked()
	if err != nil {
		return nil, err
	}
	filtered := make([]skillRepoConfig, 0, len(store.Repos))
	for _, repo := range store.Repos {
		if strings.EqualFold(repo.Owner, owner) && strings.EqualFold(repo.Name, name) {
			continue
		}
		filtered = append(filtered, repo)
	}
	if len(filtered) == 0 {
		filtered = cloneDefaultRepos()
	}
	store.Repos = filtered
	if err := ss.saveStoreLocked(store); err != nil {
		return nil, err
	}
	return cloneRepoConfigs(store.Repos), nil
}

// Internal helpers ---------------------------------------------------------

func (ss *SkillService) loadStore() (skillStore, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.loadStoreLocked()
}

func (ss *SkillService) loadStoreLocked() (skillStore, error) {
	data, err := os.ReadFile(ss.storePath)
	if err != nil {
		if os.IsNotExist(err) {
			store := skillStore{Skills: make(map[string]skillState)}
			store.ensureRepos()
			return store, nil
		}
		return skillStore{Skills: make(map[string]skillState)}, err
	}
	store := skillStore{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &store); err != nil {
			return skillStore{Skills: make(map[string]skillState)}, err
		}
	}
	if store.Skills == nil {
		store.Skills = make(map[string]skillState)
	}
	store.ensureRepos()
	return store, nil
}

func (store *skillStore) ensureRepos() {
	if len(store.Repos) == 0 {
		store.Repos = cloneDefaultRepos()
	}
	for i := range store.Repos {
		store.Repos[i] = normalizeRepoConfig(store.Repos[i])
		if !store.Repos[i].Enabled {
			store.Repos[i].Enabled = true
		}
	}
}

func cloneDefaultRepos() []skillRepoConfig {
	repos := make([]skillRepoConfig, len(defaultSkillRepos))
	copy(repos, defaultSkillRepos)
	return repos
}

func cloneRepoConfigs(repos []skillRepoConfig) []skillRepoConfig {
	copyRepos := make([]skillRepoConfig, len(repos))
	copy(copyRepos, repos)
	return copyRepos
}

func normalizeRepoConfig(repo skillRepoConfig) skillRepoConfig {
	repo.Owner = strings.TrimSpace(repo.Owner)
	repo.Name = strings.TrimSpace(repo.Name)
	repo.Branch = strings.TrimSpace(repo.Branch)
	if repo.Branch == "" {
		repo.Branch = "main"
	}
	if !repo.Enabled {
		repo.Enabled = true
	}
	return repo
}

func validateRepoConfig(repo skillRepoConfig) error {
	if repo.Owner == "" || repo.Name == "" {
		return errors.New("owner/name 不能为空")
	}
	return nil
}

func equalRepo(a, b skillRepoConfig) bool {
	return strings.EqualFold(a.Owner, b.Owner) && strings.EqualFold(a.Name, b.Name)
}

func (ss *SkillService) saveStoreLocked(store skillStore) error {
	if err := os.MkdirAll(filepath.Dir(ss.storePath), 0o755); err != nil {
		return err
	}
	store.ensureRepos()
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp := ss.storePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, ss.storePath)
}

func (ss *SkillService) prepareRepoSnapshot(repo skillRepoConfig) (string, string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "skill-repo-")
	if err != nil {
		return "", "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}
	archivePath := filepath.Join(tmpDir, "repo.zip")
	branches := buildBranchCandidates(repo.Branch)
	var lastErr error
	for _, branch := range branches {
		archiveURL := fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/%s.zip", repo.Owner, repo.Name, branch)
		if err := ss.downloadFile(archiveURL, archivePath); err != nil {
			lastErr = err
			continue
		}
		rootDir, err := unzipArchive(archivePath, tmpDir)
		if err != nil {
			lastErr = err
			continue
		}
		return rootDir, branch, cleanup, nil
	}
	cleanup()
	if lastErr == nil {
		lastErr = fmt.Errorf("无法下载仓库 %s/%s", repo.Owner, repo.Name)
	}
	return "", "", nil, lastErr
}

func buildBranchCandidates(preferred string) []string {
	set := make(map[string]struct{})
	ordered := make([]string, 0, len(defaultRepoBranches)+1)
	if preferred != "" {
		set[strings.ToLower(preferred)] = struct{}{}
		ordered = append(ordered, preferred)
	}
	for _, branch := range defaultRepoBranches {
		key := strings.ToLower(branch)
		if _, ok := set[key]; ok {
			continue
		}
		set[key] = struct{}{}
		ordered = append(ordered, branch)
	}
	return ordered
}

func (ss *SkillService) downloadFile(rawURL, dest string) error {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ai-code-studio")
	resp, err := ss.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: %s", resp.Status)
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

func unzipArchive(zipPath, dest string) (string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	var root string
	for _, file := range reader.File {
		name := file.Name
		if name == "" {
			continue
		}
		if root == "" {
			root = strings.Split(name, "/")[0]
		}
		targetPath := filepath.Join(dest, name)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return "", err
		}
		src, err := file.Open()
		if err != nil {
			return "", err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			src.Close()
			return "", err
		}
		if _, err := io.Copy(dst, src); err != nil {
			src.Close()
			dst.Close()
			return "", err
		}
		src.Close()
		dst.Close()
	}
	if root == "" {
		return "", errors.New("压缩包内容为空")
	}
	return filepath.Join(dest, root), nil
}

func (ss *SkillService) mergeLocalSkills(skills map[string]Skill) {
	entries, err := os.ReadDir(ss.installDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := entry.Name()
		dirKey := normalizeDirectoryKey(dir)
		if existing, ok := skills[dirKey]; ok {
			existing.Installed = true
			skills[dirKey] = existing
			continue
		}
		meta, err := readSkillMetadata(filepath.Join(ss.installDir, dir))
		name := strings.TrimSpace(meta.Name)
		desc := strings.TrimSpace(meta.Description)
		if err != nil || name == "" {
			name = dir
		}
		skills[dirKey] = Skill{
			Key:         buildSkillKey("", "", dir),
			Name:        name,
			Description: desc,
			Directory:   dir,
			ReadmeURL:   "",
			Installed:   true,
		}
	}
}

func (ss *SkillService) resolveReposForInstall(req installRequest, repos []skillRepoConfig) []skillRepoConfig {
	owner := strings.TrimSpace(req.RepoOwner)
	name := strings.TrimSpace(req.RepoName)
	var target []skillRepoConfig
	if owner != "" && name != "" {
		for _, repo := range repos {
			if !repo.Enabled {
				continue
			}
			if strings.EqualFold(repo.Owner, owner) && strings.EqualFold(repo.Name, name) {
				target = append(target, repo)
			}
		}
		return target
	}
	for _, repo := range repos {
		if repo.Enabled {
			target = append(target, repo)
		}
	}
	return target
}

func buildRepoURL(repo skillRepoConfig, branch, directory string) string {
	dir := strings.Trim(directory, "/")
	if dir == "" {
		return fmt.Sprintf("https://github.com/%s/%s", repo.Owner, repo.Name)
	}
	return fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s", repo.Owner, repo.Name, branch, dir)
}

func buildSkillKey(owner, name, directory string) string {
	owner = strings.ToLower(strings.TrimSpace(owner))
	name = strings.ToLower(strings.TrimSpace(name))
	directory = strings.ToLower(directory)
	if owner == "" && name == "" {
		return fmt.Sprintf("local:%s", directory)
	}
	return fmt.Sprintf("%s/%s:%s", owner, name, directory)
}

func normalizeDirectoryKey(directory string) string {
	return strings.ToLower(strings.TrimSpace(directory))
}

func (ss *SkillService) isInstalled(directory string) bool {
	info, err := os.Stat(filepath.Join(ss.installDir, directory))
	return err == nil && info.IsDir()
}

func readSkillMetadata(dir string) (skillMetadata, error) {
	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return skillMetadata{}, err
	}
	return parseSkillMetadata(string(data))
}

func parseSkillMetadata(content string) (skillMetadata, error) {
	var meta skillMetadata
	content = strings.TrimLeft(content, "\ufeff")

	// 使用 splitFrontMatter 替代 strings.SplitN，避免 YAML 值中的 --- 被误判
	_, fmLines, _, err := splitFrontMatter(content)
	if err != nil {
		return meta, errors.New("SKILL.md 缺少 front matter")
	}

	frontMatter := strings.Join(fmLines, "\n")
	if err := yaml.Unmarshal([]byte(frontMatter), &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func copyDirectory(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if rel == "." {
				return os.MkdirAll(dst, 0o755)
			}
			return os.MkdirAll(target, 0o755)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
