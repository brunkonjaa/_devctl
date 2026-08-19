package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"devctl/internal/events"
)

type CommandID string

const (
	GitStatus               CommandID = "git-status"
	GitBranch               CommandID = "git-branch"
	GitCommit               CommandID = "git-commit"
	JavaVersion             CommandID = "java-version"
	GradleBuild             CommandID = "gradle-build"
	GradleUnitTests         CommandID = "gradle-unit-tests"
	GradleLint              CommandID = "gradle-lint"
	GradleCoverage          CommandID = "gradle-coverage"
	GradleDependencies      CommandID = "gradle-dependencies"
	GradleDependenciesDebug CommandID = "gradle-dependencies-debug"
	OsvScanner              CommandID = "osv-scanner"
	OsvScannerVersion       CommandID = "osv-scanner-version"
	GoVersion               CommandID = "go-version"
	GoEnvironment           CommandID = "go-environment"
	GoRaceEnvironment       CommandID = "go-race-environment"
	GoTest                  CommandID = "go-test"
	GoTestRace              CommandID = "go-test-race"
	GoBuild                 CommandID = "go-build"
)

type Spec struct {
	ID      CommandID
	Program string
	Args    []string
	Timeout TimeoutPolicy
}

type TimeoutPolicy struct {
	Hard       time.Duration
	Inactivity time.Duration
}

var allowed = map[CommandID]Spec{
	GitStatus:               {ID: GitStatus, Program: "git", Args: []string{"status", "--short", "--branch"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GitBranch:               {ID: GitBranch, Program: "git", Args: []string{"branch", "--show-current"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GitCommit:               {ID: GitCommit, Program: "git", Args: []string{"rev-parse", "HEAD"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	JavaVersion:             {ID: JavaVersion, Program: "java", Args: []string{"-version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GradleBuild:             gradleSpec(GradleBuild, "assembleDebug", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleUnitTests:         gradleSpec(GradleUnitTests, "test", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleLint:              gradleSpec(GradleLint, "lint", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleCoverage:          gradleSpec(GradleCoverage, "jacocoTestReport", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleDependencies:      gradleDependencySpec(GradleDependencies, "releaseRuntimeClasspath"),
	GradleDependenciesDebug: gradleDependencySpec(GradleDependenciesDebug, "debugRuntimeClasspath"),
	OsvScanner:              {ID: OsvScanner, Program: "osv-scanner", Args: []string{"scan", "source", "--recursive", "--format=json", "."}, Timeout: TimeoutPolicy{Hard: 15 * time.Minute, Inactivity: 3 * time.Minute}},
	OsvScannerVersion:       {ID: OsvScannerVersion, Program: "osv-scanner", Args: []string{"--version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoVersion:               {ID: GoVersion, Program: "go", Args: []string{"version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoEnvironment:           {ID: GoEnvironment, Program: "go", Args: []string{"env", "GOOS", "GOARCH", "CGO_ENABLED"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoRaceEnvironment:       {ID: GoRaceEnvironment, Program: "go", Args: []string{"env", "GOOS", "GOARCH", "CGO_ENABLED", "CC"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoTest:                  {ID: GoTest, Program: "go", Args: []string{"test", "-count=1", "./..."}, Timeout: TimeoutPolicy{Hard: 15 * time.Minute, Inactivity: 3 * time.Minute}},
	GoTestRace:              {ID: GoTestRace, Program: "go", Args: []string{"test", "-race", "-count=1", "./..."}, Timeout: TimeoutPolicy{Hard: 20 * time.Minute, Inactivity: 3 * time.Minute}},
	GoBuild:                 {ID: GoBuild, Program: "go", Args: []string{"build", "./..."}, Timeout: TimeoutPolicy{Hard: 5 * time.Minute, Inactivity: 3 * time.Minute}},
}

func gradleSpec(id CommandID, task string, timeout TimeoutPolicy) Spec {
	if runtime.GOOS == "windows" {
		return Spec{ID: id, Program: "cmd.exe", Args: []string{"/d", "/c", "gradlew.bat", task}, Timeout: timeout}
	}
	return Spec{ID: id, Program: "./gradlew", Args: []string{task}, Timeout: timeout}
}

func gradleDependencySpec(id CommandID, configuration string) Spec {
	spec := gradleSpec(id, ":app:dependencies", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute})
	spec.Args = append(spec.Args, "--configuration", configuration, "--console=plain")
	return spec
}

type Result struct {
	Output             string
	OutputBytes        int64
	OutputTruncated    bool
	ExitCode           int
	Started            bool
	TerminationReason  string
	Executable         string
	Arguments          []string
	EnvironmentProfile string
	EnvironmentKeys    []string
	EnvironmentValues  map[string]string
}

type OutputMetricSnapshot struct {
	RawBytes      int64
	RetainedBytes int64
	Truncated     bool
}

type OutputMetrics struct {
	rawBytes      atomic.Int64
	retainedBytes atomic.Int64
	truncated     atomic.Bool
}

func (metrics *OutputMetrics) Snapshot() OutputMetricSnapshot {
	if metrics == nil {
		return OutputMetricSnapshot{}
	}
	return OutputMetricSnapshot{
		RawBytes:      metrics.rawBytes.Load(),
		RetainedBytes: metrics.retainedBytes.Load(),
		Truncated:     metrics.truncated.Load(),
	}
}

type outputMetricsContextKey struct{}

func WithOutputMetrics(ctx context.Context, metrics *OutputMetrics) context.Context {
	if metrics == nil {
		return ctx
	}
	return context.WithValue(ctx, outputMetricsContextKey{}, metrics)
}

type Compiler struct {
	Name string
	Path string
}

const maxCapturedOutput = 1024 * 1024

func Run(ctx context.Context, projectPath string, id CommandID) (Result, error) {
	spec, ok := allowed[id]
	if !ok {
		return Result{}, fmt.Errorf("command %q is not allowlisted", id)
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("project path is not a directory: %s", abs)
	}

	return RunWithOptions(ctx, projectPath, id, spec.Timeout)
}

func Available(id CommandID) bool {
	spec, ok := allowed[id]
	if !ok {
		return false
	}
	_, err := exec.LookPath(spec.Program)
	return err == nil
}

func AvailableProgram(program string) bool {
	_, err := exec.LookPath(program)
	return err == nil
}

func AvailableCompiler() (Compiler, bool) {
	for _, name := range []string{"gcc", "clang", "clang-cl"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return Compiler{Name: name, Path: path}, true
		}
	}
	return Compiler{}, false
}

func RunCompilerVersion(ctx context.Context, projectPath, compilerName string) (Result, error) {
	if compilerName != "gcc" && compilerName != "clang" && compilerName != "clang-cl" {
		return Result{}, fmt.Errorf("compiler %q is not allowlisted", compilerName)
	}
	path, err := exec.LookPath(compilerName)
	if err != nil {
		return Result{}, fmt.Errorf("resolve compiler %q: %w", compilerName, err)
	}
	spec := Spec{ID: CommandID("compiler-version-" + compilerName), Program: compilerName, Args: []string{"--version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}}
	return runSpec(ctx, projectPath, spec, spec.Timeout, sanitizedEnvironment(), "compiler-provenance", nil, path)
}

func RunWithOptions(ctx context.Context, projectPath string, id CommandID, timeout TimeoutPolicy) (Result, error) {
	spec, ok := allowed[id]
	if !ok {
		return Result{}, fmt.Errorf("command %q is not allowlisted", id)
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("project path is not a directory: %s", abs)
	}

	environment, environmentProfile, environmentValues := executionEnvironment(id)
	return runSpec(ctx, projectPath, spec, timeout, environment, environmentProfile, environmentValues, "")
}

// RunGradleDependencyGraph prefers the release runtime graph and falls back
// to the debug runtime graph for Android projects that do not define release.
func RunGradleDependencyGraph(ctx context.Context, projectPath string) (Result, error) {
	result, err := Run(ctx, projectPath, GradleDependencies)
	if err == nil {
		return result, nil
	}
	return Run(ctx, projectPath, GradleDependenciesDebug)
}

func runSpec(ctx context.Context, projectPath string, spec Spec, timeout TimeoutPolicy, environment []string, environmentProfile string, environmentValues map[string]string, resolvedExecutable string) (Result, error) {
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return Result{}, fmt.Errorf("project path is not a directory: %s", abs)
	}

	executable := resolvedExecutable
	if executable == "" {
		executable, err = resolveExecutable(spec, abs)
		if err != nil {
			return Result{}, fmt.Errorf("resolve executable %q: %w", spec.Program, err)
		}
	}
	command := exec.Command(executable, spec.Args...)
	command.Dir = abs
	command.Env = environment
	prepareCommand(command)
	output := &boundedOutput{limit: maxCapturedOutput}
	output.lastActivity.Store(time.Now().UnixNano())
	command.Stdout = &activityWriter{target: output, ctx: ctx, stream: "stdout"}
	command.Stderr = &activityWriter{target: output, ctx: ctx, stream: "stderr"}
	if err := command.Start(); err != nil {
		events.Emit(ctx, events.Event{EventType: events.ProcessFinished, Status: "ERROR", Executable: executable, Arguments: append([]string(nil), spec.Args...), Message: err.Error()})
		return Result{}, err
	}

	result := Result{Started: true, Executable: executable, Arguments: append([]string(nil), spec.Args...), EnvironmentProfile: environmentProfile, EnvironmentKeys: environmentKeys(environment), EnvironmentValues: cloneEnvironmentValues(environmentValues)}
	events.Emit(ctx, events.Event{EventType: events.ProcessStarted, Executable: executable, Arguments: append([]string(nil), spec.Args...), Message: "process started"})
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()

	termination := ""
	var runErr error
	var hardTimer <-chan time.Time
	var hardTimerObject *time.Timer
	if timeout.Hard > 0 {
		hardTimerObject = time.NewTimer(timeout.Hard)
		hardTimer = hardTimerObject.C
		defer hardTimerObject.Stop()
	}
	checkTicker := time.NewTicker(250 * time.Millisecond)
	defer checkTicker.Stop()
	for {
		select {
		case runErr = <-waitDone:
			termination = "completed"
			goto finished
		case <-ctx.Done():
			termination = "cancelled"
			terminateProcessTree(command)
			runErr = <-waitDone
			goto finished
		case <-hardTimer:
			termination = "hard_timeout"
			terminateProcessTree(command)
			runErr = <-waitDone
			goto finished
		case <-checkTicker.C:
			if timeout.Inactivity > 0 && time.Since(time.Unix(0, output.lastActivity.Load())) >= timeout.Inactivity {
				termination = "inactivity_timeout"
				terminateProcessTree(command)
				runErr = <-waitDone
				goto finished
			}
		}
	}

finished:
	result.Output = output.String()
	result.OutputBytes = output.TotalBytes()
	result.OutputTruncated = output.Truncated()
	recordOutputMetrics(ctx, result.OutputBytes, int64(len(result.Output)), result.OutputTruncated)
	result.TerminationReason = termination
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if termination != "completed" {
		events.Emit(ctx, events.Event{EventType: events.ProcessFinished, Status: "ERROR", Executable: executable, Arguments: append([]string(nil), spec.Args...), Message: termination})
		return result, fmt.Errorf("command terminated: %s", termination)
	}
	if runErr != nil {
		status := "ERROR"
		message := runErr.Error()
		if result.ExitCode != 0 {
			status = "FAIL"
			message = fmt.Sprintf("process exited with code %d", result.ExitCode)
		}
		events.Emit(ctx, events.Event{EventType: events.ProcessFinished, Status: status, Executable: executable, Arguments: append([]string(nil), spec.Args...), Message: message})
		return result, runErr
	}
	status := "PASS"
	if result.ExitCode != 0 {
		status = "FAIL"
	}
	events.Emit(ctx, events.Event{EventType: events.ProcessFinished, Status: status, Executable: executable, Arguments: append([]string(nil), spec.Args...), Message: "process finished"})
	return result, nil
}

var inheritedEnvironmentAllowlist = map[string]bool{
	"APPDATA": true, "ANDROID_HOME": true, "ANDROID_SDK_ROOT": true, "GOPATH": true, "GOROOT": true,
	"GRADLE_USER_HOME": true, "HOME": true, "HOMEDRIVE": true, "HOMEPATH": true, "LANG": true,
	"LC_ALL": true, "LOCALAPPDATA": true, "PATH": true, "PATHEXT": true, "PROGRAMDATA": true,
	"PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "SYSTEMDRIVE": true, "SYSTEMROOT": true,
	"TEMP": true, "TMP": true, "TZ": true, "USERPROFILE": true, "WINDIR": true,
}

func sanitizedEnvironment() []string {
	result := make([]string, 0)
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found && inheritedEnvironmentAllowlist[strings.ToUpper(key)] {
			result = append(result, entry)
		}
	}
	return result
}

func executionEnvironment(id CommandID) ([]string, string, map[string]string) {
	environment := sanitizedEnvironment()
	profile := "sanitized-default"
	values := make(map[string]string)
	if id == GoVersion || id == GoEnvironment || id == GoRaceEnvironment || id == GoTest || id == GoTestRace || id == GoBuild {
		environment = withEnvironmentValue(environment, "GOENV", "off")
		values["GOENV"] = "off"
		profile = "go-controlled"
	}
	if id == GoRaceEnvironment || id == GoTestRace {
		environment = withEnvironmentValue(environment, "CGO_ENABLED", "1")
		values["CGO_ENABLED"] = "1"
		profile = "go-race-controlled"
		if compiler := firstAvailableCompiler(); compiler != "" {
			environment = withEnvironmentValue(environment, "CC", compiler)
			values["CC"] = compiler
		}
	}
	return environment, profile, values
}

func firstAvailableCompiler() string {
	if compiler, ok := AvailableCompiler(); ok {
		return compiler.Name
	}
	return ""
}

func cloneEnvironmentValues(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func withEnvironmentValue(environment []string, key, value string) []string {
	upperKey := strings.ToUpper(key)
	result := make([]string, 0, len(environment)+1)
	replaced := false
	for _, entry := range environment {
		entryKey, _, found := strings.Cut(entry, "=")
		if found && strings.ToUpper(entryKey) == upperKey {
			if !replaced {
				result = append(result, key+"="+value)
				replaced = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !replaced {
		result = append(result, key+"="+value)
	}
	return result
}

func resolveExecutable(spec Spec, projectPath string) (string, error) {
	if !strings.ContainsAny(spec.Program, `/\\`) || filepath.IsAbs(spec.Program) {
		return exec.LookPath(spec.Program)
	}
	candidate, err := filepath.Abs(filepath.Join(projectPath, spec.Program))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(projectPath, candidate)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("project-relative executable escapes project path: %q", spec.Program)
	}
	if info, err := os.Stat(candidate); err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("path is a directory")
		}
		return "", err
	}
	return candidate, nil
}

func environmentKeys(environment []string) []string {
	keys := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, found := strings.Cut(entry, "=")
		if found {
			keys = append(keys, strings.ToUpper(key))
		}
	}
	sort.Strings(keys)
	return keys
}

type activityWriter struct {
	target *boundedOutput
	ctx    context.Context
	stream string
}

func (writer *activityWriter) Write(data []byte) (int, error) {
	writer.target.mu.Lock()
	defer writer.target.mu.Unlock()
	count, err := writer.target.write(data)
	if count > 0 {
		writer.target.lastActivity.Store(time.Now().UnixNano())
		message := string(data[:count])
		if len(message) > 4096 {
			message = message[:4096] + "\n[output event truncated]"
		}
		events.Emit(writer.ctx, events.Event{EventType: events.ProcessOutput, Stream: writer.stream, Message: message})
	}
	return count, err
}

type boundedOutput struct {
	mu           sync.Mutex
	data         []byte
	limit        int
	totalBytes   int64
	truncated    bool
	lastActivity atomic.Int64
}

func (output *boundedOutput) write(data []byte) (int, error) {
	output.totalBytes += int64(len(data))
	if output.limit <= len(output.data) {
		output.truncated = true
		return len(data), nil
	}
	remaining := output.limit - len(output.data)
	if len(data) > remaining {
		output.data = append(output.data, data[:remaining]...)
		output.truncated = true
		return len(data), nil
	}
	output.data = append(output.data, data...)
	return len(data), nil
}

func (output *boundedOutput) TotalBytes() int64 {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.totalBytes
}

func (output *boundedOutput) String() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return string(output.data)
}

func (output *boundedOutput) Truncated() bool {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.truncated
}

func recordOutputMetrics(ctx context.Context, rawBytes, retainedBytes int64, truncated bool) {
	metrics, ok := ctx.Value(outputMetricsContextKey{}).(*OutputMetrics)
	if !ok || metrics == nil {
		return
	}
	metrics.rawBytes.Add(rawBytes)
	metrics.retainedBytes.Add(retainedBytes)
	if truncated {
		metrics.truncated.Store(true)
	}
}

func prepareCommand(command *exec.Cmd) {
	prepareProcess(command)
}
