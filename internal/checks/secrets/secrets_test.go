package secrets

import (
	"os"
	"path/filepath"
	"testing"

	"devctl/internal/model"
)

func TestScanFindsPrivateKeyPattern(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.txt")
	fixture := "-----BEGIN " + "PRIVATE KEY-----\nexample\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	result := scan(root)
	if result.Status != model.Fail || len(result.Findings) != 1 || !result.Blocking {
		t.Fatalf("expected blocking secret finding, got %#v", result)
	}
}

func TestScanDoesNotTreatCleanPatternScanAsProof(t *testing.T) {
	result := scan(t.TempDir())
	if result.Status != model.Pass || result.Reason == "" {
		t.Fatalf("expected qualified clean result, got %#v", result)
	}
}
