//go:build integration

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	// Configure once before parallel tests so fixtures and readVersion see the same Git settings.
	if err := os.Setenv("GIT_CONFIG_NOSYSTEM", "1"); err != nil {
		log.Fatal(err)
	}
	if err := os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull); err != nil {
		log.Fatal(err)
	}
	os.Exit(m.Run())
}

func gitCommand(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepository(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	gitCommand(t, directory, "init", "-q")
	gitCommand(t, directory, "config", "user.name", "Version Test")
	gitCommand(t, directory, "config", "user.email", "version@example.invalid")
	gitCommand(t, directory, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "fixture")
	return directory
}

func checkVersion(t *testing.T, directory, explicit, want string) {
	t.Helper()
	got, err := readVersion(directory, explicit)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("version = %q, want %q", got, want)
	}
}

func TestGitVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		tag      string
		detached bool
		want     string
	}{
		{name: "clean tagless"},
		{name: "detached", detached: true},
		{name: "own annotated tag", tag: "v1.260910.0", want: "1.260910.0"},
		{name: "own rc tag", tag: "v1.260910.1-rc.2", want: "1.260910.1-rc.2"},
		{name: "upstream tag", tag: "v1.12.0"},
		{name: "similar prefix", tag: "upstream/v1.260910.0"},
		{name: "invalid suffix", tag: "v1.260910.0-beta.1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := newRepository(t)
			if test.tag != "" {
				gitCommand(t, directory, "-c", "tag.gpgsign=false", "tag", "-a", test.tag, "-m", "fixture")
			}
			if test.detached {
				gitCommand(t, directory, "checkout", "--detach", "-q", "HEAD")
			}
			want := test.want
			if want == "" {
				want = "dev-" + gitCommand(t, directory, "rev-parse", "--short", "HEAD")
			}
			checkVersion(t, directory, "", want)
			checkVersion(t, directory, "v1.260911.4", "1.260911.4")
		})
	}
}

func TestVersionIgnoresAncestorTag(t *testing.T) {
	t.Parallel()
	directory := newRepository(t)
	gitCommand(t, directory, "-c", "tag.gpgsign=false", "tag", "v1.260910.0")
	gitCommand(t, directory, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", "later")
	checkVersion(t, directory, "", "dev-"+gitCommand(t, directory, "rev-parse", "--short", "HEAD"))
}

func TestVersionMultipleReleaseTags(t *testing.T) {
	t.Parallel()
	directory := newRepository(t)
	gitCommand(t, directory, "-c", "tag.gpgsign=false", "tag", "v1.260910.0")
	gitCommand(t, directory, "-c", "tag.gpgsign=false", "tag", "v1.260910.1-rc.1")
	if version, err := readVersion(directory, ""); err == nil {
		t.Fatalf("ambiguous release tags selected version %q", version)
	}
	checkVersion(t, directory, "v1.260910.0", "1.260910.0")
}

func TestVersionArchive(t *testing.T) {
	t.Parallel()
	checkVersion(t, t.TempDir(), "", "dev-unknown")
	parent := newRepository(t)
	gitCommand(t, parent, "-c", "tag.gpgsign=false", "tag", "v1.260910.0")
	archive := filepath.Join(parent, "archive")
	if err := os.Mkdir(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	checkVersion(t, archive, "", "dev-unknown")
	checkVersion(t, archive, "v1.260911.0", "1.260911.0")
}

func TestVersionSubmodule(t *testing.T) {
	t.Parallel()
	source := newRepository(t)
	gitCommand(t, source, "-c", "tag.gpgsign=false", "tag", "v1.260910.0")
	parent := newRepository(t)
	gitCommand(t, parent, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "core")
	submodule := filepath.Join(parent, "core")
	gitCommand(t, submodule, "checkout", "--detach", "-q", "HEAD")
	info, err := os.Stat(filepath.Join(submodule, ".git"))
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("submodule .git must be a file: %v", err)
	}
	checkVersion(t, submodule, "", "1.260910.0")
	checkVersion(t, submodule, "v1.260911.0", "1.260911.0")
}
