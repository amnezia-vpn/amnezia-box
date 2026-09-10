# Building Amnezia

Use `Makefile.amnezia` for fork builds. The inherited `Makefile` and
`cmd/internal/build_libbox/main.go` remain unchanged from the selected upstream
baseline; Amnezia mobile builds use `cmd/internal/build_amnezia_libbox`.

Run these commands from the source root with Go **1.25.5** on `PATH`. The wrapper
sets `GOTOOLCHAIN=local` and `GOWORK=off`, so it uses the selected toolchain and
the checked-in modules. Use `GO=/path/to/go` to select another Go executable
explicitly. Mobile builds also need that toolchain's `bin` directory on `PATH`.

```sh
make -f Makefile.amnezia version
make -f Makefile.amnezia build
bin/amnezia-box version
make -f Makefile.amnezia test
```

The default CLI output is `bin/amnezia-box`. Its `BUILD_TAGS` retain the inherited
CLI features and include `with_awg`:

```text
with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme,with_clash_api,with_tailscale,with_awg
```

`TEST_TAGS` defaults to `BUILD_TAGS` plus `with_conntrack`. The test target also
runs the version helper's temporary-Git integration fixtures. To focus package
tests or add race detection, set `TEST_PACKAGES` and `TEST_ARGS`:

```sh
make -f Makefile.amnezia test TEST_PACKAGES='./transport/awg ./protocol/awg' TEST_ARGS=-race
```

Overriding `BUILD_TAGS`, `TEST_TAGS`, or `MOBILE_TAGS` replaces their defaults;
retain `with_awg` for Amnezia and `with_conntrack` for relevant tests.

## Versions and cross-compilation

An explicit `VERSION` takes precedence. One leading `v` is removed before the
version is embedded in the CLI or libbox:

```sh
make -f Makefile.amnezia build VERSION=v1.260910.0 OUTPUT=/tmp/amnezia-box
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 make -f Makefile.amnezia build OUTPUT=/tmp/amnezia-box-linux-amd64
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 make -f Makefile.amnezia build OUTPUT=/tmp/amnezia-box-windows-amd64.exe
```

Without `VERSION`, the helper recognizes only an exact
`v1.YYMMDD.N[-rc.K]` tag pointing at `HEAD`. It accepts annotated and lightweight
tags. Upstream tags such as `v1.12.0`, ancestor tags, and remote refs do not select
an Amnezia release. If several Amnezia release tags point at `HEAD`, set `VERSION`
explicitly to resolve the ambiguity.

An untagged checkout reports `dev-<shortsha>`, including detached checkouts and
submodules whose `.git` is a file. There is no dirty suffix: release automation
must verify its source tree separately. An archive without its own `.git` reports
`dev-unknown`, even when unpacked inside another Git repository. Explicit
`VERSION` works in archives as well. Version values may contain letters, digits,
periods, plus signs, and hyphens, starting with a letter or digit.

The version helper comes from this checkout and always runs for the host OS and
architecture, even when `GOOS` and `GOARCH` select a cross-compilation target. It
does not download or run upstream `read_tag@latest`.

## Mobile libraries

Install the pinned **SagerNet gomobile v0.1.8** and matching `gobind` using Go
1.25.5. The upstream `golang.org/x/mobile` tool is not interchangeable. The
builder locates tools in `$(go env GOPATH)/bin`; a dedicated GOPATH avoids
replacing other installed mobile tools:

```sh
export GOTOOLCHAIN=local GOWORK=off
export GOPATH="$(mktemp -d)"
go install github.com/sagernet/gomobile/cmd/gomobile@v0.1.8
go install github.com/sagernet/gomobile/cmd/gobind@v0.1.8
```

Android additionally requires OpenJDK 17, the Android SDK with accepted licenses,
and an Android NDK. Set `JAVA_HOME`, `ANDROID_HOME`, and `ANDROID_NDK_HOME` for the
installed tools. The inherited SDK finder prefers NDK `28.0.13004108` when present;
otherwise it accepts `ANDROID_NDK_HOME` and can fall back to an installed NDK with
a reproducibility warning. Record the actual SDK/NDK versions for release builds.

```sh
make -f Makefile.amnezia lib_android VERSION=v1.260910.0
make -f Makefile.amnezia lib_android LIB_TARGET=android/arm64 OUTPUT=/tmp/libbox-arm64.aar
```

The default output is `bin/libbox.aar`, built for the inherited Android
architecture set with API level 21. `LIB_TARGET` narrows the gomobile platform
list. Android keeps the common mobile features and Tailscale, and adds `with_awg`.

Apple builds require macOS, Xcode, and the SDKs for the requested platforms:

```sh
make -f Makefile.amnezia lib_apple VERSION=v1.260910.0
make -f Makefile.amnezia lib_apple LIB_TARGET=ios OUTPUT=/tmp/Libbox.xcframework
```

The default output is `bin/Libbox.xcframework` for `ios,tvos,macos`. Apple keeps
DHCP in the common Apple tags, low-memory mode on non-macOS targets, and Tailscale
on macOS. Set `LIB_FLAGS=-with-tailscale` to include Tailscale on iOS/tvOS too.
`LIB_FLAGS=-debug` selects the inherited debug profile (Android arm64 or iOS by
default); an explicit `LIB_TARGET` still takes precedence.

The wrapper adds only `MOBILE_TAGS=with_awg` to the existing mobile profiles; it
does not apply the CLI tag list to mobile. It passes the selected version and
output path to `cmd/internal/build_amnezia_libbox`. This command reuses upstream
`build_shared` SDK/mobile-tool discovery and writes only the requested output;
it has no sibling-client copying or moving. Direct invocation requires `-version`
and `-output`; the wrapper resolves both automatically. Its other flags are
`-target`, `-platform`, `-tags`, `-debug`, and `-with-tailscale`. The upstream
`cmd/internal/build_libbox` remains available with its original behavior.

Successful packaging alone does not verify an Android/iOS application's runtime
behavior. Test the produced library in the consuming application before release.
See [the consumer guide](consumer.md) for module and submodule integration.
