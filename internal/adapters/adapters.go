package adapters

import (
	"devctl/internal/adapters/androidgradle"
	"devctl/internal/adapters/golang"
	"devctl/internal/checks/secrets"
	"devctl/internal/model"
	"devctl/internal/scheduler"
)

func Checks(project model.Project) []scheduler.CheckSpec {
	checks := []scheduler.CheckSpec{androidgradle.GitStatusCheck(project), secrets.Check(project)}
	for _, technology := range project.Technologies {
		if technology.ID == "android-gradle" {
			checks = append(checks, androidgradle.Checks(project)...)
			checks = append(checks, androidgradle.AdditionalChecks(project)...)
			break
		}
		if technology.ID == "go" {
			checks = append(checks, golang.Checks(project)...)
			break
		}
	}
	return checks
}
