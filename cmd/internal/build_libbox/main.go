package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "github.com/sagernet/gomobile"
	"github.com/sagernet/sing-box/cmd/internal/build_shared"
	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/rw"
	"github.com/sagernet/sing/common/shell"
)

var (
	debugEnabled  bool
	target        string
	platform      string
	withTailscale bool
	buildVersion  string
	extraTags     string
	outputPath    string
	copyToClient  bool
)

func init() {
	flag.BoolVar(&debugEnabled, "debug", false, "enable debug")
	flag.StringVar(&target, "target", "android", "target platform")
	flag.StringVar(&platform, "platform", "", "specify platform")
	flag.BoolVar(&withTailscale, "with-tailscale", false, "build tailscale for iOS and tvOS")
	flag.StringVar(&buildVersion, "version", "", "override embedded version")
	flag.StringVar(&extraTags, "tags", "", "additional comma-separated build tags")
	flag.StringVar(&outputPath, "output", "", "output AAR or XCFramework path")
	flag.BoolVar(&copyToClient, "copy", true, "copy or move output into sibling clients")
}

func main() {
	flag.Parse()

	currentTag := strings.TrimPrefix(buildVersion, "v")
	if currentTag == "" {
		var err error
		currentTag, err = build_shared.ReadTag()
		if err != nil {
			currentTag = "unknown"
		}
	}
	configureBuild(currentTag)
	if outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			log.Fatal(E.Cause(err, "create output directory"))
		}
	}

	build_shared.FindMobile()

	switch target {
	case "android":
		buildAndroid()
	case "apple":
		buildApple()
	default:
		log.Fatal("unknown target: ", target)
	}
}

var (
	sharedFlags []string
	debugFlags  []string
	sharedTags  []string
	darwinTags  []string
	memcTags    []string
	notMemcTags []string
	debugTags   []string
)

func configureBuild(currentTag string) {
	sharedFlags = []string{"-trimpath", "-buildvcs=false"}
	sharedFlags = append(sharedFlags, "-ldflags", "-X github.com/sagernet/sing-box/constant.Version="+currentTag+" -s -w -buildid=")
	debugFlags = []string{"-ldflags", "-X github.com/sagernet/sing-box/constant.Version=" + currentTag}

	sharedTags = []string{"with_gvisor", "with_quic", "with_wireguard", "with_utls", "with_clash_api", "with_conntrack"}
	for _, tag := range strings.Split(extraTags, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			sharedTags = append(sharedTags, tag)
		}
	}
	darwinTags = []string{"with_dhcp"}
	memcTags = []string{"with_tailscale"}
	notMemcTags = []string{"with_low_memory"}
	debugTags = []string{"debug"}
}

func buildAndroid() {
	build_shared.FindSDK()

	var javaPath string
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome == "" {
		javaPath = "java"
	} else {
		javaPath = filepath.Join(javaHome, "bin", "java")
	}

	javaVersion, err := shell.Exec(javaPath, "--version").ReadOutput()
	if err != nil {
		log.Fatal(E.Cause(err, "check java version"))
	}
	if !strings.Contains(javaVersion, "openjdk 17") {
		log.Fatal("java version should be openjdk 17")
	}

	args := androidArgs()

	command := exec.Command(build_shared.GoBinPath+"/gomobile", args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err = command.Run()
	if err != nil {
		log.Fatal(err)
	}

	const name = "libbox.aar"
	nameSource := outputPath
	if nameSource == "" {
		nameSource = name
	}
	copyPath := filepath.Join("..", "sing-box-for-android", "app", "libs")
	if copyToClient && rw.IsDir(copyPath) {
		copyPath, _ = filepath.Abs(copyPath)
		destination := filepath.Join(copyPath, name)
		same, err := checkOutputDestination(nameSource, destination)
		if err != nil {
			log.Fatal(err)
		}
		if same {
			return
		}
		err = rw.CopyFile(nameSource, destination)
		if err != nil {
			log.Fatal(err)
		}
		log.Info("copied to ", copyPath)
	}
}

func buildApple() {
	args := appleArgs()

	command := exec.Command(build_shared.GoBinPath+"/gomobile", args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	err := command.Run()
	if err != nil {
		log.Fatal(err)
	}

	copyPath := filepath.Join("..", "sing-box-for-apple")
	if copyToClient && rw.IsDir(copyPath) {
		targetDir := filepath.Join(copyPath, "Libbox.xcframework")
		targetDir, _ = filepath.Abs(targetDir)
		source := outputPath
		if source == "" {
			source = "Libbox.xcframework"
		}
		same, err := checkOutputDestination(source, targetDir)
		if err != nil {
			log.Fatal(err)
		}
		if same {
			return
		}
		if err := os.RemoveAll(targetDir); err != nil {
			log.Fatal(err)
		}
		if err := os.Rename(source, targetDir); err != nil {
			log.Fatal(err)
		}
		log.Info("copied to ", targetDir)
	}
}

// checkOutputDestination prevents copying onto the source or deleting its parent.
func checkOutputDestination(source, destination string) (bool, error) {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return false, err
	}
	destinationInfo, err := os.Stat(destination)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if os.SameFile(sourceInfo, destinationInfo) {
		return true, nil
	}
	if sourceInfo.IsDir() && destinationInfo.IsDir() {
		sourcePath, err := filepath.EvalSymlinks(source)
		if err != nil {
			return false, err
		}
		sourcePath, err = filepath.Abs(sourcePath)
		if err != nil {
			return false, err
		}
		destinationPath, err := filepath.EvalSymlinks(destination)
		if err != nil {
			return false, err
		}
		destinationPath, err = filepath.Abs(destinationPath)
		if err != nil {
			return false, err
		}
		relative, err := filepath.Rel(destinationPath, sourcePath)
		if err != nil {
			return false, err
		}
		if relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return false, E.New("output is inside sibling destination: ", source)
		}
	}
	return false, nil
}

func androidArgs() []string {
	var bindTarget string
	if platform != "" {
		bindTarget = platform
	} else if debugEnabled {
		bindTarget = "android/arm64"
	} else {
		bindTarget = "android"
	}

	args := []string{
		"bind",
		"-v",
		"-target", bindTarget,
		"-androidapi", "21",
		"-javapkg=io.nekohasekai",
		"-libname=box",
	}

	if !debugEnabled {
		flags := append([]string{}, sharedFlags...)
		flags[3] += " -checklinkname=0"
		args = append(args, flags...)
	} else {
		flags := append([]string{}, debugFlags...)
		flags[1] += " -checklinkname=0"
		args = append(args, flags...)
	}

	tags := append(append([]string{}, sharedTags...), memcTags...)
	if debugEnabled {
		tags = append(tags, debugTags...)
	}

	args = append(args, "-tags", strings.Join(tags, ","))
	if outputPath != "" {
		args = append(args, "-o", outputPath)
	}
	args = append(args, "./experimental/libbox")

	return args
}

func appleArgs() []string {
	var bindTarget string
	if platform != "" {
		bindTarget = platform
	} else if debugEnabled {
		bindTarget = "ios"
	} else {
		bindTarget = "ios,tvos,macos"
	}

	args := []string{
		"bind",
		"-v",
		"-target", bindTarget,
		"-libname=box",
		"-tags-not-macos=with_low_memory",
	}
	if !withTailscale {
		args = append(args, "-tags-macos="+strings.Join(memcTags, ","))
	}

	if !debugEnabled {
		args = append(args, sharedFlags...)
	} else {
		args = append(args, debugFlags...)
	}

	tags := append(append([]string{}, sharedTags...), darwinTags...)
	if withTailscale {
		tags = append(tags, memcTags...)
	}
	if debugEnabled {
		tags = append(tags, debugTags...)
	}

	args = append(args, "-tags", strings.Join(tags, ","))
	if outputPath != "" {
		args = append(args, "-o", outputPath)
	}
	args = append(args, "./experimental/libbox")

	return args
}
