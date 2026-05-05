package vcs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"skillhub/pkg/vcs"
)

// ParseTagVersion tests

func TestParseTagVersion_Plain(t *testing.T) {
	r := vcs.ParseTagVersion("v1.2.3")
	if r != "v1.2.3" {
		t.Errorf("expected 'v1.2.3', got %q", r)
	}
}

func TestParseTagVersion_Monorepo(t *testing.T) {
	r := vcs.ParseTagVersion("social/publish-post/v1.2.3")
	if r != "v1.2.3" {
		t.Errorf("expected 'v1.2.3', got %q", r)
	}
}

func TestParseTagVersion_NonSemver(t *testing.T) {
	r := vcs.ParseTagVersion("v1.2")
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestParseTagVersion_Empty(t *testing.T) {
	r := vcs.ParseTagVersion("")
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestParseTagVersion_NoVPrefix(t *testing.T) {
	r := vcs.ParseTagVersion("1.2.3")
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestParseTagVersion_PreRelease(t *testing.T) {
	r := vcs.ParseTagVersion("v1.2.3-beta1")
	if r != "v1.2.3-beta1" {
		t.Errorf("expected 'v1.2.3-beta1', got %q", r)
	}
}

func TestParseTagVersion_NonNumericPatch(t *testing.T) {
	r := vcs.ParseTagVersion("v1.2.abc")
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

// SelectLatestVersion tests

func TestSelectLatestVersion_Normal(t *testing.T) {
	r := vcs.SelectLatestVersion([]string{"v1.0.0", "v2.0.0", "v1.5.0"})
	if r != "v2.0.0" {
		t.Errorf("expected 'v2.0.0', got %q", r)
	}
}

func TestSelectLatestVersion_FiltersPrerelease(t *testing.T) {
	r := vcs.SelectLatestVersion([]string{"v1.0.0", "v2.0.0-beta", "v2.0.0-alpha"})
	if r != "v1.0.0" {
		t.Errorf("expected 'v1.0.0', got %q", r)
	}
}

func TestSelectLatestVersion_Empty(t *testing.T) {
	r := vcs.SelectLatestVersion(nil)
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestSelectLatestVersion_NoValid(t *testing.T) {
	r := vcs.SelectLatestVersion([]string{"beta", "abc"})
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestSelectLatestVersion_AllPrerelease(t *testing.T) {
	r := vcs.SelectLatestVersion([]string{"v1.0.0-alpha", "v2.0.0-beta"})
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestSelectLatestVersion_MonorepoTags(t *testing.T) {
	r := vcs.SelectLatestVersion([]string{"foo/v1.0.0", "bar/v2.0.0", "foo/v1.5.0"})
	if r != "bar/v2.0.0" {
		t.Errorf("expected 'bar/v2.0.0', got %q", r)
	}
}

func TestSelectLatestVersion_SelectsHighestPatch(t *testing.T) {
	r := vcs.SelectLatestVersion([]string{"v1.0.0", "v1.0.1", "v1.0.10"})
	if r != "v1.0.10" {
		t.Errorf("expected 'v1.0.10', got %q", r)
	}
}

// PseudoVersion tests

func TestPseudoVersion_Format(t *testing.T) {
	hash := "abc123def456abc123def456abc123def456abc12"
	tm := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	r := vcs.PseudoVersion(hash, tm)
	expected := "v0.0.0-20240115103000-abc123def456"
	if r != expected {
		t.Errorf("expected %q, got %q", expected, r)
	}
}

func TestPseudoVersion_ShortHash(t *testing.T) {
	hash := "abc123def456"
	tm := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	r := vcs.PseudoVersion(hash, tm)
	expected := "v0.0.0-20240601000000-abc123def456"
	if r != expected {
		t.Errorf("expected %q, got %q", expected, r)
	}
}

// RepoURL tests

func TestRepoURL_Standard(t *testing.T) {
	r := vcs.RepoURL("github.com/acme/clawhub/social/publish-post")
	if r != "https://github.com/acme/clawhub" {
		t.Errorf("expected 'https://github.com/acme/clawhub', got %q", r)
	}
}

func TestRepoURL_NoSubpath(t *testing.T) {
	r := vcs.RepoURL("github.com/bob/rednote-skill")
	if r != "https://github.com/bob/rednote-skill" {
		t.Errorf("expected 'https://github.com/bob/rednote-skill', got %q", r)
	}
}

func TestRepoURL_CustomHost(t *testing.T) {
	r := vcs.RepoURL("gitlab.com/group/project/tools/skill")
	if r != "https://gitlab.com/group/project" {
		t.Errorf("expected 'https://gitlab.com/group/project', got %q", r)
	}
}

// SubdirPath tests

func TestSubdirPath_WithSubpath(t *testing.T) {
	r := vcs.SubdirPath("github.com/acme/clawhub/social/publish-post")
	if r != "social/publish-post" {
		t.Errorf("expected 'social/publish-post', got %q", r)
	}
}

func TestSubdirPath_WithoutSubpath(t *testing.T) {
	r := vcs.SubdirPath("github.com/bob/rednote-skill")
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

func TestSubdirPath_Empty(t *testing.T) {
	r := vcs.SubdirPath("")
	if r != "" {
		t.Errorf("expected '', got %q", r)
	}
}

// --- helpers for git-dependent tests ---

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	for _, c := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name test",
		"git commit --allow-empty -m initial",
	} {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\n%s", c, err, out)
		}
	}
}

func tagRepo(t *testing.T, dir string, tags ...string) {
	t.Helper()
	for _, tag := range tags {
		cmd := exec.Command("git", "tag", tag)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git tag %s failed: %v\n%s", tag, err, out)
		}
	}
}

// ListRemoteTags tests

func TestListRemoteTags_Local(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	tagRepo(t, dir, "v1.0.0", "v2.0.0")

	tags, err := vcs.ListRemoteTags(dir)
	if err != nil {
		t.Fatalf("ListRemoteTags: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d: %v", len(tags), tags)
	}
}

func TestListRemoteTags_NoTags(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)

	tags, err := vcs.ListRemoteTags(dir)
	if err != nil {
		t.Fatalf("ListRemoteTags: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d", len(tags))
	}
}

// Clone tests

func TestClone_Normal(t *testing.T) {
	requireGit(t)
	srcDir := t.TempDir()
	initRepo(t, srcDir)
	tagRepo(t, srcDir, "v1.0.0")

	targetDir := t.TempDir()
	cloneTarget := filepath.Join(targetDir, "out")

	err := vcs.Clone(srcDir, "v1.0.0", "", cloneTarget)
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	cmd := exec.Command("git", "-C", cloneTarget, "log", "--oneline")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("verify clone failed: %v\n%s", err, out)
	}
}

func TestClone_Monorepo(t *testing.T) {
	requireGit(t)
	srcDir := t.TempDir()
	initRepo(t, srcDir)

	subDir := filepath.Join(srcDir, "skills", "my-skill")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "SKILL.md"), []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cmd := exec.Command("sh", "-c", "git add . && git commit --allow-empty -m 'add skill'")
	cmd.Dir = srcDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, out)
	}
	tagRepo(t, srcDir, "v1.0.0")

	targetDir := t.TempDir()
	err := vcs.Clone(srcDir, "v1.0.0", "skills/my-skill", targetDir)
	if err != nil {
		t.Fatalf("Clone monorepo failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "SKILL.md")); os.IsNotExist(err) {
		t.Errorf("SKILL.md not found in target after monorepo clone")
	}
}

// ResolveVersion tests

func TestResolveVersion_ExactConstraint(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	tagRepo(t, dir, "v1.0.0", "v2.0.0", "v3.0.0")

	version, err := vcs.ResolveVersion(dir, "v2.0.0")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if version != "v2.0.0" {
		t.Errorf("expected 'v2.0.0', got %q", version)
	}
}

func TestResolveVersion_Latest(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	tagRepo(t, dir, "v1.0.0", "v2.0.0", "v1.5.0")

	version, err := vcs.ResolveVersion(dir, "")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if version != "v2.0.0" {
		t.Errorf("expected 'v2.0.0', got %q", version)
	}
}

func TestResolveVersion_NoTags(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)

	version, err := vcs.ResolveVersion(dir, "")
	if err != nil {
		t.Fatalf("ResolveVersion: %v", err)
	}
	if !strings.HasPrefix(version, "v0.0.0-") {
		t.Errorf("expected pseudo-version prefix 'v0.0.0-', got %q", version)
	}
	if len(version) < 20 {
		t.Errorf("pseudo-version too short: %q", version)
	}
}

func TestResolveVersion_ConstraintNotFound(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	initRepo(t, dir)
	tagRepo(t, dir, "v1.0.0")

	_, err := vcs.ResolveVersion(dir, "v9.9.9")
	if err == nil {
		t.Fatal("expected error for missing constraint, got nil")
	}
}
