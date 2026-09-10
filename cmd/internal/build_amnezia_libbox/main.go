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

	"github.com/sagernet/sing-box/cmd/internal/build_shared"
	"github.com/sagernet/sing-box/log"
)

type buildOptions struct {
	target        string
	platform      string
	version       string
	tags          string
	output        string
	debug         bool
	withTailscale bool
}

func main() {
	var options buildOptions
	flag.StringVar(&options.target, "target", "android", "android or apple")
	flag.StringVar(&options.platform, "platform", "", "gomobile target platform list")
	flag.StringVar(&options.version, "version", "", "explicit embedded version (required)")
	flag.StringVar(&options.tags, "tags", "with_awg", "additional comma-separated build tags")
	flag.StringVar(&options.output, "output", "", "output AAR or XCFramework path (required)")
	flag.BoolVar(&options.debug, "debug", false, "enable debug profile")
	flag.BoolVar(&options.withTailscale, "with-tailscale", false, "build tailscale for iOS and tvOS")
	flag.Parse()
	if err := build(options); err != nil {
		log.Fatal(err)
	}
}

func build(options buildOptions) error {
	args, err := buildArgs(options)
	if err != nil {
		return err
	}
	build_shared.FindMobile()
	if options.target == "android" {
		build_shared.FindSDK()
		javaPath := "java"
		if javaHome := os.Getenv("JAVA_HOME"); javaHome != "" {
			javaPath = filepath.Join(javaHome, "bin", "java")
		}
		javaVersion, err := exec.Command(javaPath, "--version").Output()
		if err != nil {
			return fmt.Errorf("check java version: %w", err)
		}
		if !strings.Contains(string(javaVersion), "openjdk 17") {
			return errors.New("java version should be openjdk 17")
		}
	}
	if err := os.MkdirAll(filepath.Dir(options.output), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	command := exec.Command(filepath.Join(build_shared.GoBinPath, "gomobile"), args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("build mobile library: %w", err)
	}
	return nil
}

func buildArgs(options buildOptions) ([]string, error) {
	version := strings.TrimPrefix(options.version, "v")
	if !regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]*$`).MatchString(version) {
		return nil, errors.New("an explicit version without spaces is required")
	}
	if options.output == "" {
		return nil, errors.New("an explicit output path is required")
	}
	tags := []string{
		"with_gvisor", "with_quic", "with_wireguard", "with_utls", "with_clash_api", "with_conntrack",
	}
	for _, tag := range strings.Split(options.tags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	args := []string{"bind", "-v", "-libname=box"}
	var platform string
	switch options.target {
	case "android":
		platform = "android"
		if options.debug {
			platform = "android/arm64"
		}
		args = append(args, "-androidapi", "21", "-javapkg=io.nekohasekai")
		tags = append(tags, "with_tailscale")
	case "apple":
		platform = "ios,tvos,macos"
		if options.debug {
			platform = "ios"
		}
		tags = append(tags, "with_dhcp")
		args = append(args, "-tags-not-macos=with_low_memory")
		if options.withTailscale {
			tags = append(tags, "with_tailscale")
		} else {
			args = append(args, "-tags-macos=with_tailscale")
		}
	default:
		return nil, fmt.Errorf("unknown target: %s", options.target)
	}
	if options.platform != "" {
		platform = options.platform
	}
	args = append(args, "-target", platform)
	ldflags := "-X github.com/sagernet/sing-box/constant.Version=" + version
	if options.debug {
		tags = append(tags, "debug")
	} else {
		args = append(args, "-trimpath", "-buildvcs=false")
		ldflags += " -s -w -buildid="
	}
	if options.target == "android" {
		ldflags += " -checklinkname=0"
	}
	args = append(args,
		"-ldflags", ldflags, "-tags", strings.Join(tags, ","),
		"-o", options.output, "./experimental/libbox",
	)
	return args, nil
}
