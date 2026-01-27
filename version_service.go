package main

const AppVersion = "v2.6.23"

// UpdatePolicy 更新策略（可通过 -ldflags "-X main.UpdatePolicy=installer" 覆盖）
// 可选值：auto（默认）、portable、installer
var UpdatePolicy = "auto"

type VersionService struct {
	version string
}

func NewVersionService() *VersionService {
	return &VersionService{version: AppVersion}
}

func (vs *VersionService) CurrentVersion() string {
	return vs.version
}

// GetUpdatePolicy 获取更新策略
func (vs *VersionService) GetUpdatePolicy() string {
	return UpdatePolicy
}
