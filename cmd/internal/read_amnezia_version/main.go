package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	releaseTag   = regexp.MustCompile(`^v1\.[0-9]{6}\.[0-9]+(-rc\.[0-9]+)?$`)
	versionValue = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`)
)

func main() {
	version := flag.String("version", "", "explicit version (optional leading v is removed)")
	directory := flag.String("directory", ".", "source checkout root")
	flag.Parse()
	value, err := readVersion(*directory, *version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(value)
}

func readVersion(directory, explicit string) (string, error) {
	if explicit != "" {
		version := strings.TrimPrefix(explicit, "v")
		if !versionValue.MatchString(version) {
			return "", fmt.Errorf("invalid version %q", explicit)
		}
		return version, nil
	}
	// Requiring the root marker accepts worktrees and submodules (.git files),
	// while preventing an unpacked archive from inheriting a parent repo's tag.
	if _, err := os.Stat(filepath.Join(directory, ".git")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "dev-unknown", nil
		}
		return "", fmt.Errorf("inspect source git marker: %w", err)
	}
	gitOutput := func(args ...string) (string, error) {
		command := exec.Command("git", args...)
		command.Dir = directory
		output, err := command.Output()
		return strings.TrimSpace(string(output)), err
	}
	commit, err := gitOutput("rev-parse", "--short", "HEAD")
	if err != nil {
		return "dev-unknown", nil
	}
	tags, err := gitOutput("tag", "--points-at", "HEAD")
	if err != nil {
		return "", fmt.Errorf("read source tags: %w", err)
	}
	version := ""
	for _, tag := range strings.Fields(tags) {
		if !releaseTag.MatchString(tag) {
			continue
		}
		if version != "" {
			return "", errors.New("multiple amnezia release tags at HEAD; set VERSION explicitly")
		}
		version = strings.TrimPrefix(tag, "v")
	}
	if version != "" {
		return version, nil
	}
	return "dev-" + commit, nil
}
