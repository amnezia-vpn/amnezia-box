//go:build integration

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMobileCLI(t *testing.T) {
	if os.Getenv("AMNEZIA_BUILD_TEST_CHILD") == "1" {
		os.Args = []string{
			"build_amnezia_libbox", "-target", os.Getenv("AMNEZIA_BUILD_TEST_TARGET"),
			"-version", "v1.260910.3", "-tags", "with_awg",
			"-output", os.Getenv("AMNEZIA_BUILD_TEST_OUTPUT"),
		}
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake gomobile fixture uses a POSIX shell")
	}
	for _, target := range []string{"android", "apple"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			core := filepath.Join(root, "core")
			bin := filepath.Join(root, "tools", "bin")
			sdk := filepath.Join(root, "sdk")
			ndk := filepath.Join(sdk, "ndk", "28.0.13004108")
			if err := os.MkdirAll(core, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFixture := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeFixture(filepath.Join(bin, "gobind"), "#!/bin/sh\nexit 0\n")
			writeFixture(filepath.Join(bin, "java"), "#!/bin/sh\nprintf 'openjdk 17\\n'\n")
			writeFixture(filepath.Join(sdk, "licenses", "android-sdk-license"), "fixture\n")
			writeFixture(filepath.Join(ndk, "source.properties"), "fixture\n")
			writeFixture(filepath.Join(bin, "gomobile"), `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$AMNEZIA_BUILD_TEST_ARGS"
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then output="$2"; shift; fi
  shift
done
test -n "$output"
test -d "$(dirname "$output")"
if [ "$AMNEZIA_BUILD_TEST_TARGET" = apple ]; then
  mkdir -p "$output"
  output="$output/payload"
fi
printf 'built artifact\n' > "$output"
`)
			writeFixture(filepath.Join(bin, "git"), "#!/bin/sh\ntouch \"$AMNEZIA_BUILD_TEST_GIT\"\nexit 1\n")
			markers := []string{
				filepath.Join(root, "sing-box-for-android", "app", "libs", "libbox.aar"),
				filepath.Join(root, "sing-box-for-apple", "Libbox.xcframework", "payload"),
			}
			for _, marker := range markers {
				writeFixture(marker, "existing client\n")
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			name := "custom.aar"
			if target == "apple" {
				name = "Custom.xcframework"
			}
			output := filepath.Join(root, "artifacts with spaces", name)
			argsPath := filepath.Join(root, "args")
			gitPath := filepath.Join(root, "git-called")
			command := exec.Command(executable, "-test.run=^TestMobileCLI$")
			command.Dir = core
			command.Env = append(os.Environ(),
				"AMNEZIA_BUILD_TEST_CHILD=1", "AMNEZIA_BUILD_TEST_TARGET="+target,
				"GOPATH="+filepath.Join(root, "tools"), "JAVA_HOME="+filepath.Join(root, "tools"),
				"ANDROID_HOME="+sdk, "ANDROID_NDK_HOME="+ndk,
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"AMNEZIA_BUILD_TEST_OUTPUT="+output, "AMNEZIA_BUILD_TEST_ARGS="+argsPath,
				"AMNEZIA_BUILD_TEST_GIT="+gitPath,
			)
			if result, err := command.CombinedOutput(); err != nil {
				t.Fatalf("mobile CLI: %v: %s", err, result)
			}
			args, err := os.ReadFile(argsPath)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(args), "constant.Version=1.260910.3") || !strings.Contains(string(args), "with_awg") {
				t.Fatalf("version or extra tags missing: %s", args)
			}
			if target == "apple" {
				output = filepath.Join(output, "payload")
			}
			content, err := os.ReadFile(output)
			if err != nil || string(content) != "built artifact\n" {
				t.Fatalf("requested output missing or changed: %q, %v", content, err)
			}
			for _, marker := range markers {
				content, err := os.ReadFile(marker)
				if err != nil || string(content) != "existing client\n" {
					t.Fatalf("sibling client changed: %q, %v", content, err)
				}
			}
			if _, err := os.Stat(gitPath); !os.IsNotExist(err) {
				t.Fatalf("explicit version unexpectedly invoked git: %v", err)
			}
		})
	}
}
