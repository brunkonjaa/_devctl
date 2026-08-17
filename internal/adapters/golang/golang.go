package golang

import (
	"context"
	"fmt"
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
		return model.CheckResult{
			ID:         "go-environment",
			Status:     model.Pass,
			Summary:    "Go environment available",
			Reason:     strings.TrimSpace(version.Output + "\n" + env.Output),
			RawOutput:  version.Output + env.Output,
			Executions: []model.Execution{executionFromResult(version), executionFromResult(env)},
		}
	}
}

func raceCheck(project model.Project) scheduler.CheckFunc {
	return func(ctx context.Context) model.CheckResult {
		environment, err := runner.Run(ctx, project.Path, runner.GoRaceEnvironment)
		if err != nil {
			return model.CheckResult{ID: "go-test-race", Status: model.Error, Summary: "Go race environment could not be inspected", Reason: err.Error(), Executions: []model.Execution{executionFromResult(environment)}}
		}
		lines := strings.Fields(environment.Output)
		cgo := ""
		if len(lines) >= 3 {
			cgo = lines[2]
		}
		available, reason := raceEnvironmentFor(runtime.GOOS, cgo, runner.AvailableProgram)
		if !available {
			return model.CheckResult{ID: "go-test-race", Status: model.NotTested, Summary: "Go race test was not run", Reason: reason, Executions: []model.Execution{executionFromResult(environment)}}
		}
		compiler, compilerAvailable := runner.AvailableCompiler()
		if !compilerAvailable {
			return model.CheckResult{ID: "go-test-race", Status: model.NotTested, Summary: "Go race test was not run", Reason: "no supported C compiler (gcc, clang, or clang-cl) was found", Executions: []model.Execution{executionFromResult(environment)}}
		}
		compilerVersion, compilerErr := runner.RunCompilerVersion(ctx, project.Path, compiler.Name)
		if compilerErr != nil {
			return model.CheckResult{ID: "go-test-race", Status: model.Error, Summary: "Go race compiler could not be inspected", Reason: compilerErr.Error(), Executions: []model.Execution{executionFromResult(environment), executionFromResult(compilerVersion)}}
		}
		compilerText := strings.TrimSpace(strings.SplitN(compilerVersion.Output, "\n", 2)[0])
		environmentExecution := executionFromResult(environment)
		environmentExecution.CompilerName = compiler.Name
		environmentExecution.CompilerPath = compilerVersion.Executable
		environmentExecution.CompilerVersion = compilerText
		compilerExecution := executionFromResult(compilerVersion)
		compilerExecution.CompilerName = compiler.Name
		compilerExecution.CompilerPath = compilerVersion.Executable
		compilerExecution.CompilerVersion = compilerText
		raceResult, raceErr := runner.Run(ctx, project.Path, runner.GoTestRace)
		check := commandResult(string(runner.GoTestRace), raceResult, raceErr, "Go race test could not be completed")
		raceExecution := executionFromResult(raceResult)
		raceExecution.CompilerName = compiler.Name
		raceExecution.CompilerPath = compilerVersion.Executable
		raceExecution.CompilerVersion = compilerText
		check.Executions = []model.Execution{environmentExecution, compilerExecution, raceExecution}
		return check
	}
}

func buildCheck(project model.Project) scheduler.CheckFunc {
	return commandCheck(project, runner.GoBuild, "Go build")
}

func commandCheck(project model.Project, command runner.CommandID, label string) scheduler.CheckFunc {
	return func(ctx context.Context) model.CheckResult {
		result, err := runner.Run(ctx, project.Path, command)
		return commandResult(string(command), result, err, label+" could not be completed")
	}
}

func commandResult(id string, result runner.Result, err error, failureSummary string) model.CheckResult {
	check := model.CheckResult{ID: id, RawOutput: result.Output, OutputTruncated: result.OutputTruncated, Executable: result.Executable, Arguments: result.Arguments, EnvironmentProfile: result.EnvironmentProfile, EnvironmentKeys: result.EnvironmentKeys, ExitCode: &result.ExitCode}
	if result.Started || result.Executable != "" {
		check.Executions = []model.Execution{executionFromResult(result)}
	}
	if err != nil {
		if result.Started && result.TerminationReason == "completed" && result.ExitCode != 0 {
			check.Status = model.Fail
			check.Summary = strings.TrimSuffix(failureSummary, " could not be completed") + " failed"
			check.Reason = fmt.Sprintf("process exited with code %d", result.ExitCode)
			return check
		}
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

func executionFromResult(result runner.Result) model.Execution {
	return model.Execution{
		Executable:         result.Executable,
		Arguments:          append([]string(nil), result.Arguments...),
		EnvironmentProfile: result.EnvironmentProfile,
		EnvironmentKeys:    append([]string(nil), result.EnvironmentKeys...),
		EnvironmentValues:  cloneStringMap(result.EnvironmentValues),
		OutputTruncated:    result.OutputTruncated,
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
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
