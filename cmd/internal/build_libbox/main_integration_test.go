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
			"build_libbox", "-target", "apple", "-version", "v1.260910.3",
			"-tags", "with_awg", "-output", os.Getenv("AMNEZIA_BUILD_TEST_OUTPUT"), "-copy=false",
		}
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake gomobile fixture uses a POSIX shell")
	}
	root := t.TempDir()
	core := filepath.Join(root, "core")
	bin := filepath.Join(root, "tools", "bin")
	client := filepath.Join(root, "sing-box-for-apple", "Libbox.xcframework")
	for _, directory := range []string{core, bin, client} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(filepath.Join(bin, "gobind"), "#!/bin/sh\nexit 0\n")
	writeFixture(filepath.Join(bin, "gomobile"), "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$AMNEZIA_BUILD_TEST_ARGS\"\ntouch \"$AMNEZIA_BUILD_TEST_OUTPUT\"\n")
	writeFixture(filepath.Join(bin, "git"), "#!/bin/sh\ntouch \"$AMNEZIA_BUILD_TEST_GIT\"\nexit 1\n")
	marker := filepath.Join(client, "keep")
	writeFixture(marker, "existing client\n")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "artifacts", "custom.xcframework")
	argsPath := filepath.Join(root, "args")
	gitPath := filepath.Join(root, "git-called")
	command := exec.Command(executable, "-test.run=^TestMobileCLI$")
	command.Dir = core
	command.Env = append(os.Environ(),
		"AMNEZIA_BUILD_TEST_CHILD=1", "GOPATH="+filepath.Join(root, "tools"),
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
	for _, path := range []string{output, marker} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("output or existing client changed: %v", err)
		}
	}
	if _, err := os.Stat(gitPath); !os.IsNotExist(err) {
		t.Fatalf("explicit version unexpectedly invoked git: %v", err)
	}
}

func TestMobileOutputCopy(t *testing.T) {
	if os.Getenv("AMNEZIA_OUTPUT_TEST_CHILD") == "1" {
		os.Args = []string{"build_libbox", "-target", os.Getenv("AMNEZIA_OUTPUT_TEST_TARGET"), "-version", "1.260910.0"}
		if output := os.Getenv("AMNEZIA_OUTPUT_TEST_PATH"); output != "" {
			os.Args = append(os.Args, "-output", output)
		}
		main()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake gomobile fixture uses a POSIX shell")
	}
	for _, target := range []string{"android", "apple"} {
		for _, mode := range []string{"default", "separate", "absolute", "relative", "symlink", "hardlink", "nested", "nested_symlink"} {
			if (target == "android" && strings.HasPrefix(mode, "nested")) || (target == "apple" && mode == "hardlink") {
				continue
			}
			t.Run(target+"/"+mode, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				core := filepath.Join(root, "core")
				bin := filepath.Join(root, "tools", "bin")
				sdk := filepath.Join(root, "sdk")
				ndk := filepath.Join(sdk, "ndk", "28.0.13004108")
				name := "libbox.aar"
				client := filepath.Join(root, "sing-box-for-android", "app", "libs")
				if target == "apple" {
					name = "Libbox.xcframework"
					client = filepath.Join(root, "sing-box-for-apple")
				}
				for _, directory := range []string{core, bin, client} {
					if err := os.MkdirAll(directory, 0o755); err != nil {
						t.Fatal(err)
					}
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
				artifactFile := func(path string) string {
					if target == "apple" {
						return filepath.Join(path, "payload")
					}
					return path
				}
				destination := filepath.Join(client, name)
				writeFixture(artifactFile(destination), "previous artifact\n")
				output := destination
				source := destination
				switch mode {
				case "default":
					output = ""
					source = filepath.Join(core, name)
				case "separate":
					output = filepath.Join(root, "artifacts", name)
					source = output
				case "relative":
					var err error
					output, err = filepath.Rel(core, destination)
					if err != nil {
						t.Fatal(err)
					}
				case "symlink", "hardlink":
					output = filepath.Join(root, "alias-"+name)
					source = output
					link := os.Symlink
					if mode == "hardlink" {
						link = os.Link
					}
					if err := link(destination, output); err != nil {
						t.Fatal(err)
					}
				case "nested", "nested_symlink":
					source = filepath.Join(destination, "nested.xcframework")
					output = source
					if mode == "nested_symlink" {
						if err := os.MkdirAll(source, 0o755); err != nil {
							t.Fatal(err)
						}
						output = filepath.Join(root, "alias.xcframework")
						if err := os.Symlink(source, output); err != nil {
							t.Fatal(err)
						}
					}
				}
				writeFixture(filepath.Join(bin, "gobind"), "#!/bin/sh\nexit 0\n")
				writeFixture(filepath.Join(bin, "gomobile"), `#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then output="$2"; shift; fi
  shift
done
if [ "$AMNEZIA_OUTPUT_TEST_TARGET" = apple ]; then
  output=${output:-Libbox.xcframework}
  mkdir -p "$output"
  output="$output/payload"
else
  output=${output:-libbox.aar}
fi
mkdir -p "$(dirname "$output")"
printf 'built mobile artifact\n' > "$output"
`)
				writeFixture(filepath.Join(bin, "java"), "#!/bin/sh\nprintf 'openjdk 17\\n'\n")
				writeFixture(filepath.Join(sdk, "licenses", "android-sdk-license"), "fixture\n")
				writeFixture(filepath.Join(ndk, "source.properties"), "fixture\n")
				executable, err := os.Executable()
				if err != nil {
					t.Fatal(err)
				}
				command := exec.Command(executable, "-test.run=^TestMobileOutputCopy$")
				command.Dir = core
				command.Env = append(os.Environ(),
					"AMNEZIA_OUTPUT_TEST_CHILD=1", "AMNEZIA_OUTPUT_TEST_TARGET="+target,
					"AMNEZIA_OUTPUT_TEST_PATH="+output, "GOPATH="+filepath.Join(root, "tools"),
					"JAVA_HOME="+filepath.Join(root, "tools"), "ANDROID_HOME="+sdk, "ANDROID_NDK_HOME="+ndk,
				)
				result, err := command.CombinedOutput()
				wantFailure := strings.HasPrefix(mode, "nested")
				if (err != nil) != wantFailure {
					t.Errorf("mobile builder error = %v, wantFailure = %v: %s", err, wantFailure, result)
				}
				if wantFailure && !strings.Contains(string(result), "inside") {
					t.Errorf("nested output must be rejected before removing the destination: %s", result)
				}
				checkContent := func(path, want string) {
					t.Helper()
					got, err := os.ReadFile(artifactFile(path))
					if err != nil || string(got) != want {
						t.Errorf("artifact %s = %q, error = %v, want %q", path, got, err, want)
					}
				}
				if wantFailure {
					checkContent(source, "built mobile artifact\n")
					checkContent(destination, "previous artifact\n")
				} else {
					checkContent(destination, "built mobile artifact\n")
					if target == "android" || (mode != "default" && mode != "separate") {
						checkContent(source, "built mobile artifact\n")
					} else if _, err := os.Stat(source); !os.IsNotExist(err) {
						t.Errorf("Apple output was not moved: %v", err)
					}
				}
			})
		}
	}
}
