package golang

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"devctl/internal/model"
	"devctl/internal/runner"
	"devctl/internal/scheduler"
)

const checkVersion = "go-pack-v1"

var timeout = scheduler.TimeoutPolicy{Hard: 20 * time.Minute, Inactivity: 3 * time.Minute}

func Checks(project model.Project) []scheduler.CheckSpec {
	return []scheduler.CheckSpec{
		{
			ID: "go-environment", Version: checkVersion, Timeout: scheduler.TimeoutPolicy{Hard: time.Minute, Inactivity: 30 * time.Second},
			Run: environmentCheck(project),
		},
		{
			ID: "go-test", Version: checkVersion, Requires: []string{"go-environment"}, Timeout: timeout,
			Run: commandCheck(project, runner.GoTest, "Go tests"),
		},
		{
			ID: "go-test-race", Version: checkVersion, Requires: []string{"go-environment"}, Timeout: timeout,
			Run: raceCheck(project),
		},
		{
			ID: "go-build", Version: checkVersion, Requires: []string{"go-environment"}, Timeout: timeout,
			Run: buildCheck(project),
		},
	}
}

func environmentCheck(project model.Project) scheduler.CheckFunc {
	return func(ctx context.Context) model.CheckResult {
		version, versionErr := runner.Run(ctx, project.Path, runner.GoVersion)
		if versionErr != nil {
			return commandResult("go-environment", version, versionErr, "Go environment could not be inspected")
		}
		env, envErr := runner.Run(ctx, project.Path, runner.GoEnvironment)
		if envErr != nil {
			return commandResult("go-environment", env, envErr, "Go environment could not be inspected")
		}
		return model.CheckResult{ID: "go-environment", Status: model.Pass, Summary: "Go environment available", Reason: strings.TrimSpace(version.Output + "\n" + env.Output), RawOutput: version.Output + env.Output}
	}
}

func raceCheck(project model.Project) scheduler.CheckFunc {
	return func(ctx context.Context) model.CheckResult {
		available, reason := raceEnvironmentWithGo(ctx, project)
		if !available {
			return model.CheckResult{ID: "go-test-race", Status: model.NotTested, Summary: "Go race test was not run", Reason: reason}
		}
		return commandCheck(project, runner.GoTestRace, "Go race test")(ctx)
	}
}

func buildCheck(project model.Project) scheduler.CheckFunc {
	return func(ctx context.Context) model.CheckResult {
		if err := os.MkdirAll(filepath.Join(project.Path, ".devctl", "bin"), 0700); err != nil {
			return model.CheckResult{ID: "go-build", Status: model.Error, Summary: "Go build output directory could not be created", Reason: err.Error()}
		}
		return commandCheck(project, runner.GoBuild, "Go build")(ctx)
	}
}

func commandCheck(project model.Project, command runner.CommandID, label string) scheduler.CheckFunc {
	return func(ctx context.Context) model.CheckResult {
		result, err := runner.Run(ctx, project.Path, command)
		return commandResult(string(command), result, err, label+" could not be completed")
	}
}

func commandResult(id string, result runner.Result, err error, failureSummary string) model.CheckResult {
	check := model.CheckResult{ID: id, RawOutput: result.Output, ExitCode: &result.ExitCode}
	if err != nil {
		check.Status = model.Error
		check.Summary = failureSummary
		check.Reason = err.Error()
		return check
	}
	if result.ExitCode != 0 {
		check.Status = model.Fail
		check.Summary = strings.TrimSuffix(failureSummary, " could not be completed") + " failed"
		check.Reason = fmt.Sprintf("process exited with code %d", result.ExitCode)
		return check
	}
	check.Status = model.Pass
	check.Summary = strings.TrimSuffix(failureSummary, " could not be completed") + " passed"
	return check
}

func raceEnvironmentWithGo(ctx context.Context, project model.Project) (bool, string) {
	result, err := runner.Run(ctx, project.Path, runner.GoEnvironment)
	if err != nil {
		return false, "Go environment could not be inspected: " + err.Error()
	}
	lines := strings.Fields(result.Output)
	cgo := ""
	if len(lines) > 0 {
		cgo = lines[len(lines)-1]
	}
	return raceEnvironmentFor(runtime.GOOS, cgo, runner.AvailableProgram)
}

func raceEnvironmentFor(goos, cgo string, availableProgram func(string) bool) (bool, string) {
	if goos == "js" || goos == "wasip1" {
		return false, "the Go race detector is not supported on this platform"
	}
	if strings.EqualFold(cgo, "0") {
		return false, "CGO_ENABLED=0; the Go race detector requires cgo"
	}
	for _, compiler := range []string{"gcc", "clang", "clang-cl"} {
		if availableProgram(compiler) {
			return true, ""
		}
	}
	return false, "no supported C compiler (gcc, clang, or clang-cl) was found"
}
