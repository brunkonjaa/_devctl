package androidgradle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"devctl/internal/model"
	"devctl/internal/runner"
	"devctl/internal/scheduler"
)

var gradleTimeout = scheduler.TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}

func Checks(project model.Project) []scheduler.CheckSpec {
	return []scheduler.CheckSpec{
		{
			ID: "android-java-environment",
			Run: func(ctx context.Context) model.CheckResult {
				return commandCheck(ctx, project, runner.JavaVersion, "Java is available")
			},
		},
		{
			ID: "android-gradle-wrapper",
			Run: func(context.Context) model.CheckResult {
				wrapper := "gradlew"
				if runtime.GOOS == "windows" {
					wrapper = "gradlew.bat"
				}
				return fileCheck(project.Path, "android-gradle-wrapper", []string{wrapper}, "Gradle wrapper is present")
			},
		},
		{
			ID: "android-project-structure",
			Run: func(context.Context) model.CheckResult {
				return androidProjectStructureCheck(project.Path)
			},
		},
		{
			ID:        "android-build",
			Requires:  []string{"android-java-environment", "android-gradle-wrapper", "android-project-structure"},
			Resources: []string{"gradle"},
			Timeout:   gradleTimeout,
			Run: func(ctx context.Context) model.CheckResult {
				return commandCheck(ctx, project, runner.GradleBuild, "Gradle debug build completed")
			},
		},
		{
			ID:        "android-unit-tests",
			Requires:  []string{"android-build"},
			Resources: []string{"gradle"},
			Timeout:   gradleTimeout,
			Run: func(ctx context.Context) model.CheckResult {
				result := commandCheck(ctx, project, runner.GradleUnitTests, "Android unit tests completed")
				if result.Status == model.Pass {
					result.Summary = testSummary(result.RawOutput)
				}
				return result
			},
		},
		{
			ID:        "android-lint",
			Requires:  []string{"android-java-environment", "android-gradle-wrapper", "android-project-structure"},
			Resources: []string{"gradle"},
			Timeout:   gradleTimeout,
			Run: func(ctx context.Context) model.CheckResult {
				return commandCheck(ctx, project, runner.GradleLint, "Android lint completed")
			},
		},
	}
}

func androidProjectStructureCheck(root string) model.CheckResult {
	if _, err := os.Stat(filepath.Join(root, "app")); err != nil {
		return model.CheckResult{ID: "android-project-structure", Status: model.Fail, Summary: "Android project structure is incomplete", Reason: "missing: app"}
	}
	for _, settings := range []string{"settings.gradle.kts", "settings.gradle"} {
		if _, err := os.Stat(filepath.Join(root, settings)); err == nil {
			return model.CheckResult{ID: "android-project-structure", Status: model.Pass, Summary: "Android project structure is present"}
		}
	}
	return model.CheckResult{ID: "android-project-structure", Status: model.Fail, Summary: "Android project structure is incomplete", Reason: "missing: settings.gradle.kts or settings.gradle"}
}

func GitStatusCheck(project model.Project) scheduler.CheckSpec {
	return scheduler.CheckSpec{
		ID: "git-status",
		Run: func(ctx context.Context) model.CheckResult {
			result, err := runner.Run(ctx, project.Path, runner.GitStatus)
			check := model.CheckResult{ID: "git-status", RawOutput: result.Output, OutputTruncated: result.OutputTruncated, Executable: result.Executable, Arguments: result.Arguments, EnvironmentProfile: result.EnvironmentProfile, EnvironmentKeys: result.EnvironmentKeys, Executions: executionList(result), Evidence: []model.Evidence{{Type: "git-status", Detail: strings.TrimSpace(result.Output)}}}
			if err != nil {
				check.Status = model.Error
				check.Summary = "Git status could not be collected"
				check.Reason = err.Error()
				return check
			}
			if gitOutputIsClean(result.Output) {
				check.Status = model.Pass
				check.Summary = "Git repository status collected"
				return check
			}
			check.Status = model.Warn
			check.Summary = "Git repository has uncommitted changes"
			return check
		},
	}
}

func commandCheck(ctx context.Context, project model.Project, command runner.CommandID, passSummary string) model.CheckResult {
	result, err := runner.Run(ctx, project.Path, command)
	check := model.CheckResult{RawOutput: result.Output, OutputTruncated: result.OutputTruncated, Executable: result.Executable, Arguments: result.Arguments, EnvironmentProfile: result.EnvironmentProfile, EnvironmentKeys: result.EnvironmentKeys, Executions: executionList(result), Evidence: []model.Evidence{{Type: "process-output", Detail: strings.TrimSpace(result.Output)}}}
	if err != nil {
		if result.TerminationReason != "" && result.TerminationReason != "completed" {
			check.Status = model.Error
			check.Summary = fmt.Sprintf("%s did not complete", string(command))
			check.Reason = result.TerminationReason
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			check.Status = model.Error
			check.Summary = fmt.Sprintf("%s did not complete", string(command))
			check.Reason = err.Error()
		} else if result.ExitCode != 0 {
			check.Status = model.Fail
			check.Summary = fmt.Sprintf("%s failed", string(command))
		} else {
			check.Status = model.Error
			check.Summary = fmt.Sprintf("%s could not be started", string(command))
		}
		if check.Reason == "" {
			check.Reason = err.Error()
		}
		return check
	}
	check.Status = model.Pass
	check.Summary = passSummary
	return check
}

func fileCheck(root, id string, paths []string, summary string) model.CheckResult {
	missing := make([]string, 0)
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		return model.CheckResult{ID: id, Status: model.Fail, Summary: summary + " is incomplete", Reason: "missing: " + strings.Join(missing, ", ")}
	}
	return model.CheckResult{ID: id, Status: model.Pass, Summary: summary}
}

func executionList(result runner.Result) []model.Execution {
	if !result.Started && result.Executable == "" {
		return nil
	}
	return []model.Execution{{
		Executable:         result.Executable,
		Arguments:          append([]string(nil), result.Arguments...),
		EnvironmentProfile: result.EnvironmentProfile,
		EnvironmentKeys:    append([]string(nil), result.EnvironmentKeys...),
		OutputTruncated:    result.OutputTruncated,
	}}
}

func gitOutputIsClean(output string) bool {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	return len(lines) == 1 && strings.HasPrefix(strings.TrimSpace(lines[0]), "##")
}

var testResultPattern = regexp.MustCompile(`(?i)([0-9]+) tests completed, ([0-9]+) failed`)

func testSummary(output string) string {
	match := testResultPattern.FindStringSubmatch(output)
	if len(match) == 3 {
		total, totalErr := strconv.Atoi(match[1])
		failed, failedErr := strconv.Atoi(match[2])
		if totalErr == nil && failedErr == nil && failed <= total {
			return fmt.Sprintf("Android unit tests completed — %d/%d passed", total-failed, total)
		}
	}
	return "Android unit tests completed"
}
