package gitstate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFingerprintChangesWhenDirtyFileBytesChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source.go")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("worktree fingerprint did not change with file bytes")
	}
}

func TestFingerprintIgnoresGitIgnoredBuildOutputs(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("build/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "-c", "user.name=devctl-test", "-c", "user.email=devctl-test@example.invalid", "commit", "-m", "baseline")

	first, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "build", "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "reports", "large.xml"), []byte("generated"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("ignored build output changed fingerprint: %s != %s", first, second)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
