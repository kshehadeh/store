package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	t                    *testing.T
	repo, remote, script string
	env                  []string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	source, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return newFixtureFrom(t, source, filepath.Join(source, "scripts", "release.sh"))
}

func newFixtureFrom(t *testing.T, source, script string) fixture {
	t.Helper()
	root := t.TempDir()
	f := fixture{t: t, repo: filepath.Join(root, "repo"), remote: filepath.Join(root, "origin.git"), script: script}
	runGit(t, source, "clone", "--quiet", "--bare", "--no-hardlinks", "--no-tags", source, f.remote)
	runGit(t, root, "clone", "--quiet", "--no-tags", f.remote, f.repo)
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	mock := `#!/bin/sh
case "$1:${TASK_TEST_ACTION:-}" in
  check:fail) exit 1 ;;
  check:dirty) printf changed > changed-during-checks ;;
  check:tag) git update-ref refs/tags/v9.0.0 HEAD; git push --quiet origin refs/tags/v9.0.0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "task"), []byte(mock), 0o700); err != nil {
		t.Fatal(err)
	}
	f.env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

func TestFixtureIgnoresExistingReleaseTags(t *testing.T) {
	source := newFixture(t)
	runGit(t, source.remote, "update-ref", "refs/tags/v8.0.0", "HEAD")
	f := newFixtureFrom(t, source.remote, source.script)
	if tags := runGit(t, f.remote, "tag", "--list"); tags != "" {
		t.Fatalf("fixture inherited real release tags: %s", tags)
	}
	out, err := f.run("patch", "--dry-run")
	if err != nil || !strings.Contains(out, "Next release: v0.1.0\n") {
		t.Fatalf("preview with a tagged source: %v\n%s", err, out)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func (f fixture) run(args ...string) (string, error) {
	f.t.Helper()
	cmd := exec.Command("bash", append([]string{f.script}, args...)...)
	cmd.Dir, cmd.Env = f.repo, f.env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestVersionBumps(t *testing.T) {
	for _, tc := range []struct {
		tag, mode, want string
	}{
		{"", "patch", "v0.1.0"},
		{"", "minor", "v0.1.0"},
		{"", "major", "v0.1.0"},
		{"v0.1.0", "patch", "v0.1.1"},
		{"v0.1.9", "patch", "v0.1.10"},
		{"v0.1.9", "minor", "v0.2.0"},
		{"v0.1.9", "major", "v1.0.0"},
		{"v1.9.9", "patch", "v1.9.10"},
		{"v1.9.9", "minor", "v1.10.0"},
		{"v1.9.9", "major", "v2.0.0"},
	} {
		t.Run(tc.tag+"/"+tc.mode, func(t *testing.T) {
			f := newFixture(t)
			if tc.tag != "" {
				runGit(t, f.repo, "update-ref", "refs/tags/"+tc.tag, "HEAD")
				runGit(t, f.repo, "push", "--quiet", "origin", "refs/tags/"+tc.tag)
			}
			// Prerelease tags are not the stable-version baseline.
			runGit(t, f.repo, "update-ref", "refs/tags/v99.0.0-rc.1", "HEAD")
			before := runGit(t, f.repo, "tag", "--list")
			out, err := f.run(tc.mode, "--dry-run")
			if err != nil || !strings.Contains(out, "Next release: "+tc.want+"\n") || !strings.Contains(out, "no tag was created or pushed") {
				t.Fatalf("preview: %v\n%s", err, out)
			}
			if after := runGit(t, f.repo, "tag", "--list"); after != before {
				t.Fatal("dry run created a tag")
			}
		})
	}
}

func TestReleaseGuards(t *testing.T) {
	for _, tc := range []struct {
		name, action, want string
	}{
		{"dirty", "", "working tree must be clean"},
		{"branch", "", "run releases from main"},
		{"detached", "", "run releases from main"},
		{"checks", "fail", ""},
		{"changed-tree", "dirty", "working tree must be clean"},
		{"changed-tags", "tag", "release tags changed"},
		{"local-tag", "", "has not been pushed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			switch tc.name {
			case "dirty":
				if err := os.WriteFile(filepath.Join(f.repo, "untracked"), []byte("test"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "branch":
				runGit(t, f.repo, "switch", "-c", "feature")
			case "detached":
				runGit(t, f.repo, "switch", "--detach")
			case "local-tag":
				runGit(t, f.repo, "update-ref", "refs/tags/v0.1.0", "HEAD")
			}
			f.env = append(f.env, "TASK_TEST_ACTION="+tc.action)
			out, err := f.run("patch", "--dry-run")
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("guard: %v\n%s", err, out)
			}
			if tags := runGit(t, f.remote, "tag", "--list"); strings.Contains(tags, "v0.1.0") {
				t.Fatal("failed validation published a release tag")
			}
		})
	}
}

func TestInvalidReleaseArguments(t *testing.T) {
	f := newFixture(t)
	for _, args := range [][]string{{}, {"wrong"}, {"patch", "--unknown"}, {"patch", "--dry-run", "extra"}} {
		if out, err := f.run(args...); err == nil {
			t.Fatalf("accepted arguments %v: %s", args, out)
		}
	}
}

func TestPublishToIsolatedRemote(t *testing.T) {
	f := newFixture(t)
	// Annotated tags use the configured Git identity, never a test override.
	// CI may not have an identity; dry-run and guard tests still run there.
	cmd := exec.Command("git", "var", "GIT_COMMITTER_IDENT")
	cmd.Dir = f.repo
	if err := cmd.Run(); err != nil {
		t.Skip("annotated-tag test needs a configured Git identity")
	}
	out, err := f.run("patch")
	if err != nil || !strings.Contains(out, "Pushed v0.1.0") {
		t.Fatalf("sandbox publication: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(runGit(t, f.remote, "tag", "--list")); got != "v0.1.0" {
		t.Fatalf("sandbox remote tags = %q", got)
	}
	if got := strings.TrimSpace(runGit(t, f.repo, "cat-file", "-t", "refs/tags/v0.1.0")); got != "tag" {
		t.Fatalf("release tag is not annotated: %q", got)
	}
}
