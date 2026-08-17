package adapters

import (
	"context"
	"strings"

	"devctl/internal/adapters/androidgradle"
	"devctl/internal/adapters/golang"
	"devctl/internal/checks/secrets"
	"devctl/internal/model"
	"devctl/internal/scheduler"
)

func Checks(project model.Project) []scheduler.CheckSpec {
	checks := []scheduler.CheckSpec{androidgradle.GitStatusCheck(project), secrets.Check(project)}
	unsupported := make([]string, 0)
	for _, technology := range project.Technologies {
		if technology.ID == "android-gradle" {
			checks = append(checks, androidgradle.Checks(project)...)
			checks = append(checks, androidgradle.AdditionalChecks(project)...)
			continue
		}
		if technology.ID == "go" {
			checks = append(checks, golang.Checks(project)...)
			continue
		}
		unsupported = append(unsupported, technology.ID)
	}
	if len(unsupported) > 0 {
		checks = append(checks, scheduler.CheckSpec{
			ID: "adapter-support",
			Run: func(context.Context) model.CheckResult {
				return model.CheckResult{Status: model.NotTested, Blocking: true, Summary: "Not all detected project technologies have verification adapters", Reason: strings.Join(unsupported, ", ")}
			},
		})
	}
	return checks
}
