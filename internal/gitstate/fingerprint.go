package gitstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Fingerprint covers the working files, Git identity, and index delta. It is
// deliberately independent of the dirty boolean: two dirty trees with the
// same file names but different bytes produce different fingerprints.
func Fingerprint(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return "", os.ErrInvalid
	}
	hasher := sha256.New()
	writeField(hasher, "head", git(abs, "rev-parse", "HEAD"))
	writeField(hasher, "branch", git(abs, "branch", "--show-current"))
	writeField(hasher, "status", git(abs, "status", "--porcelain=v1", "--untracked-files=all"))
	writeField(hasher, "index", git(abs, "diff", "--cached", "--binary"))
	err = filepath.WalkDir(abs, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(abs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") || rel == ".devctl" || strings.HasPrefix(rel, ".devctl/") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		writeField(hasher, "path", rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		writeField(hasher, "mode", info.Mode().String())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			writeField(hasher, "link", target)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeField(hasher, "bytes", string(data))
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func writeField(hasher hash.Hash, name, value string) {
	_, _ = hasher.Write([]byte(name))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(value))
	_, _ = hasher.Write([]byte{0})
}

func git(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	data, err := command.Output()
	if err != nil {
		return ""
	}
	return string(data)
}
