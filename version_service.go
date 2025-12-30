package main

const AppVersion = "v2.6.9"

type VersionService struct {
	version string
}

func NewVersionService() *VersionService {
	return &VersionService{version: AppVersion}
}

func (vs *VersionService) CurrentVersion() string {
	return vs.version
}
