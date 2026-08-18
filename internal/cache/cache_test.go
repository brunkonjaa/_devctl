package cache

import (
	"testing"
)

func TestCacheHitAndFingerprintInvalidation(t *testing.T) {
	root := t.TempDir()
	fingerprint := Fingerprint{ProjectID: "p", Head: "abc", Files: map[string]string{"main.go": "one"}, Config: "c", Policy: "p", Check: "v1", Devctl: "d"}
	if _, err := Put(root, "verify", fingerprint, map[string]string{"status": "PASS"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Get(root, "verify", fingerprint); err != nil || !ok {
		t.Fatalf("expected valid cache hit: %v %t", err, ok)
	}
	fingerprint.Head = "changed"
	if _, ok, err := Get(root, "verify", fingerprint); err != nil || ok {
		t.Fatalf("expected HEAD invalidation: %v %t", err, ok)
	}
}

func TestCacheClearIsExplicit(t *testing.T) {
	root := t.TempDir()
	if _, err := Put(root, "x", Fingerprint{ProjectID: "p"}, map[string]bool{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(root); err != nil {
		t.Fatal(err)
	}
	entries, err := List(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cache was not cleared: %v %+v", err, entries)
	}
}
