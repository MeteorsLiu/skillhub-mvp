package vcs

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

func ParseTagVersion(tag string) string {
	if idx := strings.LastIndex(tag, "/"); idx >= 0 {
		tag = tag[idx+1:]
	}
	if !strings.HasPrefix(tag, "v") {
		return ""
	}
	suffix := tag[1:]
	parts := strings.SplitN(suffix, ".", 3)
	if len(parts) != 3 {
		return ""
	}
	if !isNumeric(parts[0]) || !isNumeric(parts[1]) {
		return ""
	}
	patchPart := parts[2]
	patchParts := strings.SplitN(patchPart, "-", 2)
	if !isNumeric(patchParts[0]) {
		return ""
	}
	return "v" + suffix
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func SelectLatestVersion(tags []string) string {
	var bestTag string
	var bestVer string
	for _, t := range tags {
		v := ParseTagVersion(t)
		if v == "" || strings.Contains(v, "-") {
			continue
		}
		if bestTag == "" || semver.Compare(v, bestVer) > 0 {
			bestTag = t
			bestVer = v
		}
	}
	return bestTag
}

func PseudoVersion(commitHash string, commitTime time.Time) string {
	ts := commitTime.Format("20060102150405")
	hash := commitHash
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return fmt.Sprintf("v0.0.0-%s-%s", ts, hash)
}

func pseudoVersionCommit(version string) string {
	parts := strings.Split(version, "-")
	if len(parts) < 3 {
		return ""
	}
	ts := parts[len(parts)-2]
	hash := parts[len(parts)-1]
	if len(ts) != 14 || len(hash) < 12 {
		return ""
	}
	for _, c := range ts {
		if c < '0' || c > '9' {
			return ""
		}
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return ""
		}
	}
	return hash
}

func RepoURL(id string) string {
	parts := strings.SplitN(id, "/", 4)
	if len(parts) < 3 {
		return ""
	}
	return "https://" + parts[0] + "/" + parts[1] + "/" + parts[2]
}

func SubdirPath(id string) string {
	parts := strings.SplitN(id, "/", 4)
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

func Clone(repoURL, version, subDir, targetDir string) error {
	if commit := pseudoVersionCommit(version); commit != "" {
		if subDir == "" {
			return cloneCommit(repoURL, commit, targetDir)
		}
		return sparseCloneSubdirCommit(repoURL, commit, subDir, targetDir)
	}

	args := []string{"clone", "--depth", "1", "--branch", version, repoURL}
	if subDir == "" {
		args = append(args, targetDir)
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git clone failed: %w\n%s", err, out)
		}
		return nil
	}

	return sparseCloneSubdir(repoURL, version, subDir, targetDir)
}

func cloneCommit(repoURL, commit, targetDir string) error {
	cmd := exec.Command("git", "clone", "--filter=blob:none", repoURL, targetDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone failed: %w\n%s", err, out)
	}
	cmd = exec.Command("git", "-C", targetDir, "checkout", "--detach", commit)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w\n%s", err, out)
	}
	return nil
}

func sparseCloneSubdir(repoURL, version, subDir, targetDir string) error {
	return sparseCloneSubdirRef(repoURL, version, subDir, targetDir, true)
}

func sparseCloneSubdirCommit(repoURL, commit, subDir, targetDir string) error {
	return sparseCloneSubdirRef(repoURL, commit, subDir, targetDir, false)
}

func sparseCloneSubdirRef(repoURL, ref, subDir, targetDir string, shallow bool) error {
	tmpDir, err := os.MkdirTemp("", "vcs-clone-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	runGit := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s failed: %w\n%s", args[0], err, out)
		}
		return nil
	}

	gitPath := filepath.ToSlash(filepath.Clean(subDir))
	if err := runGit("init"); err != nil {
		return err
	}
	if err := runGit("remote", "add", "origin", repoURL); err != nil {
		return err
	}
	if err := runGit("sparse-checkout", "init", "--no-cone"); err != nil {
		return err
	}
	if err := runGit("sparse-checkout", "set", gitPath, gitPath+"/**"); err != nil {
		return err
	}
	if shallow {
		if err := runGit("fetch", "--depth=1", "--filter=blob:none", "origin", ref); err != nil {
			return err
		}
		if err := runGit("checkout", "FETCH_HEAD"); err != nil {
			return err
		}
	} else {
		if err := runGit("fetch", "--filter=blob:none", "origin"); err != nil {
			return err
		}
		if err := runGit("checkout", "--detach", ref); err != nil {
			return err
		}
	}

	src := filepath.Join(tmpDir, subDir)
	if err := copyDir(src, targetDir); err != nil {
		return err
	}
	return initCopiedGitMetadata(targetDir, repoURL, subDir)
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat %s: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0644)
	})
}

func initCopiedGitMetadata(targetDir, repoURL, subDir string) error {
	for _, args := range [][]string{
		{"init"},
		{"remote", "add", "origin", repoURL},
		{"config", "skillhub.subdir", filepath.ToSlash(filepath.Clean(subDir))},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = targetDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s failed: %w\n%s", args[0], err, out)
		}
	}
	return nil
}

func ListRemoteTags(repoURL string) ([]string, error) {
	cmd := exec.Command("git", "ls-remote", "--tags", repoURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-remote failed: %w\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var tags []string
	seen := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		ref := parts[1]
		if !strings.HasPrefix(ref, "refs/tags/") {
			continue
		}
		tag := ref[len("refs/tags/"):]
		if strings.HasSuffix(tag, "^{}") {
			tag = strings.TrimSuffix(tag, "^{}")
			if seen[tag] {
				continue
			}
		}
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func ResolveVersion(repoURL string, constraint string) (string, error) {
	tags, err := ListRemoteTags(repoURL)
	if err != nil {
		return "", err
	}
	if constraint != "" {
		for _, t := range tags {
			if t == constraint {
				return t, nil
			}
		}
		return "", fmt.Errorf("tag %q not found in %s", constraint, repoURL)
	}
	latest := SelectLatestVersion(tags)
	if latest != "" {
		return latest, nil
	}
	tmpDir, err := os.MkdirTemp("", "vcs-pseudo-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)
	cmd := exec.Command("git", "clone", "--depth", "1", repoURL, tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone for pseudo-version failed: %w\n%s", err, out)
	}
	cmd = exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = tmpDir
	hashOut, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\n%s", err, hashOut)
	}
	hash := strings.TrimSpace(string(hashOut))
	cmd = exec.Command("git", "log", "-1", "--format=%cd", "--date=format:%Y%m%d%H%M%S")
	cmd.Dir = tmpDir
	timeOut, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git log failed: %w\n%s", err, timeOut)
	}
	timeStr := strings.TrimSpace(string(timeOut))
	tm, err := time.Parse("20060102150405", timeStr)
	if err != nil {
		return "", fmt.Errorf("parsing commit time %q: %w", timeStr, err)
	}
	return PseudoVersion(hash, tm), nil
}
