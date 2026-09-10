package main

import (
	"slices"
	"strings"
	"testing"
)

func argumentValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	index := slices.Index(args, flag)
	if index == -1 || index+1 >= len(args) {
		t.Fatalf("missing %s in %v", flag, args)
	}
	return args[index+1]
}

func TestMobileBuildArguments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		apple      bool
		debug      bool
		tailscale  bool
		platform   string
		wantTarget string
	}{
		{name: "android release", wantTarget: "android"},
		{name: "android debug", debug: true, wantTarget: "android/arm64"},
		{name: "android platform", platform: "android/amd64", wantTarget: "android/amd64"},
		{name: "android debug platform", debug: true, platform: "android/amd64", wantTarget: "android/amd64"},
		{name: "apple release", apple: true, wantTarget: "ios,tvos,macos"},
		{name: "apple debug", apple: true, debug: true, wantTarget: "ios"},
		{name: "apple tailscale", apple: true, tailscale: true, wantTarget: "ios,tvos,macos"},
		{name: "apple platform", apple: true, platform: "macos/arm64", wantTarget: "macos/arm64"},
		{name: "apple debug platform", apple: true, debug: true, platform: "macos/arm64", wantTarget: "macos/arm64"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := buildOptions{
				target: "android", platform: test.platform, version: "v1.260910.0",
				tags: "with_awg,custom_feature", output: "artifacts/custom.aar",
				debug: test.debug, withTailscale: test.tailscale,
			}
			if test.apple {
				options.target = "apple"
				options.output = "artifacts/custom.xcframework"
			}
			args, err := buildArgs(options)
			if err != nil {
				t.Fatal(err)
			}
			if test.apple {
				if !slices.Contains(args, "-tags-not-macos=with_low_memory") {
					t.Fatal("Apple low-memory profile lost")
				}
				if slices.Contains(args, "-tags-macos=with_tailscale") == test.tailscale {
					t.Fatalf("macOS Tailscale profile does not match requested mode: %v", args)
				}
			}
			if got := argumentValue(t, args, "-target"); got != test.wantTarget {
				t.Fatalf("target = %q, want %q", got, test.wantTarget)
			}
			if got := argumentValue(t, args, "-o"); got != options.output {
				t.Fatalf("output = %q, want %q", got, options.output)
			}
			tags := strings.Split(argumentValue(t, args, "-tags"), ",")
			for _, required := range []string{"with_gvisor", "with_quic", "with_wireguard", "with_utls", "with_clash_api", "with_conntrack", "with_awg", "custom_feature"} {
				if !slices.Contains(tags, required) {
					t.Errorf("missing tag %s", required)
				}
			}
			if got := slices.Contains(tags, "with_tailscale"); got != (!test.apple || test.tailscale) {
				t.Errorf("unexpected common tailscale tag: %v", got)
			}
			if slices.Contains(tags, "with_low_memory") {
				t.Errorf("low-memory tag leaked into common tags: %v", tags)
			}
			if slices.Contains(tags, "with_dhcp") != test.apple {
				t.Errorf("DHCP tag does not match Apple profile: %v", tags)
			}
			if slices.Contains(tags, "debug") != test.debug {
				t.Error("debug tag does not match requested mode")
			}
			ldflags := argumentValue(t, args, "-ldflags")
			if !strings.Contains(ldflags, "constant.Version=1.260910.0") {
				t.Errorf("explicit version missing from %q", ldflags)
			}
			if strings.Contains(ldflags, "-checklinkname=0") == test.apple {
				t.Error("linkname flag does not match Android profile")
			}
			if strings.Contains(ldflags, " -s -w ") == test.debug {
				t.Error("linker stripping does not match requested mode")
			}
		})
	}
}

func TestMobileBuildRequiresExplicitInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		options buildOptions
	}{
		{name: "missing version", options: buildOptions{target: "apple", output: "Libbox.xcframework"}},
		{name: "empty normalized version", options: buildOptions{target: "apple", version: "v", output: "Libbox.xcframework"}},
		{name: "invalid version", options: buildOptions{target: "apple", version: "1.0 -X other=value", output: "Libbox.xcframework"}},
		{name: "missing output", options: buildOptions{target: "apple", version: "1.260910.0"}},
		{name: "unknown target", options: buildOptions{target: "other", version: "1.260910.0", output: "artifact"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := buildArgs(test.options); err == nil {
				t.Fatal("expected invalid build inputs to fail before invoking tools")
			}
		})
	}
}
