package gitstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	paths, err := trackedAndUnignoredPaths(abs)
	if err != nil {
		return "", err
	}
	for _, rel := range paths {
		if rel == ".devctl" || strings.HasPrefix(rel, ".devctl/") {
			continue
		}
		path := filepath.Join(abs, filepath.FromSlash(rel))
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			// Deleted tracked files are already represented by the Git status
			// fields above. They have no current file bytes to fingerprint.
			continue
		}
		if statErr != nil {
			return "", statErr
		}
		writeField(hasher, "path", rel)
		writeField(hasher, "mode", info.Mode().String())
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return "", readErr
			}
			writeField(hasher, "link", target)
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return "", readErr
		}
		writeField(hasher, "bytes", string(data))
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func trackedAndUnignoredPaths(root string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "-C", root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	data, err := command.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Keep the helper useful for non-Git synthetic projects. Real project
		// verification uses the Git path above, which excludes ignored build
		// outputs and dependency caches from the expensive byte walk.
		return walkFallback(root)
	}
	values := strings.Split(string(data), "\x00")
	paths := make([]string, 0, len(values))
	for _, value := range values {
		value = filepath.ToSlash(value)
		if value != "" {
			paths = append(paths, value)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func walkFallback(root string) ([]string, error) {
	paths := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ".devctl" || strings.HasPrefix(rel, ".devctl/") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk project files for fingerprint: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
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
