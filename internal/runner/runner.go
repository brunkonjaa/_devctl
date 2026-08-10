package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type CommandID string

const (
	GitStatus         CommandID = "git-status"
	GitBranch         CommandID = "git-branch"
	GitCommit         CommandID = "git-commit"
	JavaVersion       CommandID = "java-version"
	GradleBuild       CommandID = "gradle-build"
	GradleUnitTests   CommandID = "gradle-unit-tests"
	GradleLint        CommandID = "gradle-lint"
	GradleCoverage    CommandID = "gradle-coverage"
	OsvScanner        CommandID = "osv-scanner"
	OsvScannerVersion CommandID = "osv-scanner-version"
	GoVersion         CommandID = "go-version"
	GoEnvironment     CommandID = "go-environment"
	GoTest            CommandID = "go-test"
	GoTestRace        CommandID = "go-test-race"
	GoBuild           CommandID = "go-build"
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
	GitStatus:         {ID: GitStatus, Program: "git", Args: []string{"status", "--short", "--branch"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GitBranch:         {ID: GitBranch, Program: "git", Args: []string{"branch", "--show-current"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GitCommit:         {ID: GitCommit, Program: "git", Args: []string{"rev-parse", "HEAD"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	JavaVersion:       {ID: JavaVersion, Program: "java", Args: []string{"-version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GradleBuild:       gradleSpec(GradleBuild, "assembleDebug", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleUnitTests:   gradleSpec(GradleUnitTests, "test", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleLint:        gradleSpec(GradleLint, "lint", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	GradleCoverage:    gradleSpec(GradleCoverage, "jacocoTestReport", TimeoutPolicy{Hard: 35 * time.Minute, Inactivity: 3 * time.Minute}),
	OsvScanner:        {ID: OsvScanner, Program: "osv-scanner", Args: []string{"scan", "source", "--recursive", "--format=json", "."}, Timeout: TimeoutPolicy{Hard: 15 * time.Minute, Inactivity: 3 * time.Minute}},
	OsvScannerVersion: {ID: OsvScannerVersion, Program: "osv-scanner", Args: []string{"--version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoVersion:         {ID: GoVersion, Program: "go", Args: []string{"version"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoEnvironment:     {ID: GoEnvironment, Program: "go", Args: []string{"env", "GOOS", "GOARCH", "CGO_ENABLED"}, Timeout: TimeoutPolicy{Hard: 30 * time.Second, Inactivity: 30 * time.Second}},
	GoTest:            {ID: GoTest, Program: "go", Args: []string{"test", "./..."}, Timeout: TimeoutPolicy{Hard: 15 * time.Minute, Inactivity: 3 * time.Minute}},
	GoTestRace:        {ID: GoTestRace, Program: "go", Args: []string{"test", "-race", "./..."}, Timeout: TimeoutPolicy{Hard: 20 * time.Minute, Inactivity: 3 * time.Minute}},
	GoBuild:           {ID: GoBuild, Program: "go", Args: []string{"build", "-o", filepath.Join(".devctl", "bin", "devctl-selfcheck"), "./cmd/devctl"}, Timeout: TimeoutPolicy{Hard: 5 * time.Minute, Inactivity: 3 * time.Minute}},
}

func gradleSpec(id CommandID, task string, timeout TimeoutPolicy) Spec {
	if runtime.GOOS == "windows" {
		return Spec{ID: id, Program: "cmd.exe", Args: []string{"/d", "/c", "gradlew.bat", task}, Timeout: timeout}
	}
	return Spec{ID: id, Program: "./gradlew", Args: []string{task}, Timeout: timeout}
}

type Result struct {
	Output            string
	ExitCode          int
	Started           bool
	TerminationReason string
}

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

	command := exec.Command(spec.Program, spec.Args...)
	command.Dir = abs
	prepareCommand(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return Result{}, err
	}
	if err := command.Start(); err != nil {
		return Result{}, err
	}

	result := Result{Started: true}
	var output bytes.Buffer
	var outputMu sync.Mutex
	lastActivity := atomic.Int64{}
	lastActivity.Store(time.Now().UnixNano())
	copyOutput := func(reader io.Reader) error {
		writer := activityWriter{target: &output, mu: &outputMu, lastActivity: &lastActivity}
		_, copyErr := io.Copy(writer, reader)
		return copyErr
	}
	copyDone := make(chan error, 2)
	go func() { copyDone <- copyOutput(stdout) }()
	go func() { copyDone <- copyOutput(stderr) }()
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
			if timeout.Inactivity > 0 && time.Since(time.Unix(0, lastActivity.Load())) >= timeout.Inactivity {
				termination = "inactivity_timeout"
				terminateProcessTree(command)
				runErr = <-waitDone
				goto finished
			}
		}
	}

finished:
	<-copyDone
	<-copyDone
	outputMu.Lock()
	result.Output = output.String()
	outputMu.Unlock()
	result.TerminationReason = termination
	if command.ProcessState != nil {
		result.ExitCode = command.ProcessState.ExitCode()
	}
	if termination != "completed" {
		return result, fmt.Errorf("command terminated: %s", termination)
	}
	if runErr != nil {
		return result, runErr
	}
	return result, nil
}

type activityWriter struct {
	target       *bytes.Buffer
	mu           *sync.Mutex
	lastActivity *atomic.Int64
}

func (writer activityWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	count, err := writer.target.Write(data)
	if count > 0 {
		writer.lastActivity.Store(time.Now().UnixNano())
	}
	return count, err
}

func prepareCommand(command *exec.Cmd) {
	prepareProcess(command)
}
