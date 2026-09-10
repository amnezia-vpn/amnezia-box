# Using Amnezia from another project

The module path remains `github.com/sagernet/sing-box`. Keep existing imports.
Use Go 1.25.5 and build with `with_awg` (and `with_gvisor` for the userspace TUN).
The fork already requires `github.com/amnezia-vpn/amneziawg-go/v3`; do not add a
separate replacement for AWG.

## Remote Go replacement

Select an immutable Amnezia release tag. For example, after that release exists:

```sh
export GOWORK=off GOTOOLCHAIN=local
tag=v1.260910.0
go mod edit -replace="github.com/sagernet/sing-box=github.com/amnezia-vpn/amnezia-box@$tag"
go mod tidy
go build -tags with_gvisor,with_awg ./...
go list -m -json github.com/sagernet/sing-box
```

The final command must show a `Replace` pointing to
`github.com/amnezia-vpn/amnezia-box` at the selected version. Commit the consumer's
`go.mod` and `go.sum`. Other application features may need additional build tags;
see [the build profiles](build.md).

For a published commit without a tag, let Go derive the pseudo-version. Do not
invent its timestamp:

```sh
sha=<full-published-commit>
version=$(go list -m -f '{{.Version}}' "github.com/amnezia-vpn/amnezia-box@$sha")
go mod edit -replace="github.com/sagernet/sing-box=github.com/amnezia-vpn/amnezia-box@$version"
go mod tidy
```

## Git submodule and local Go replacement

A submodule pins the source commit; the replacement tells Go to use that local
module. Make these changes in the **consumer repository**:

```sh
git submodule add https://github.com/amnezia-vpn/amnezia-box.git third_party/amnezia-box
git -C third_party/amnezia-box fetch origin tag v1.260910.0
git -C third_party/amnezia-box checkout --detach v1.260910.0
go mod edit -replace=github.com/sagernet/sing-box=./third_party/amnezia-box
GOWORK=off GOTOOLCHAIN=local go mod tidy
git add .gitmodules third_party/amnezia-box go.mod go.sum
git commit -m 'Pin Amnezia source dependency'
```

In consumer CI, use `actions/checkout` with `submodules: recursive`, or run
`git submodule update --init --recursive` after checkout. Verify a new clone:

```sh
git clone --recurse-submodules <consumer-url> consumer-clean
cd consumer-clean
GOWORK=off GOTOOLCHAIN=local go build -tags with_gvisor,with_awg ./...
git -C third_party/amnezia-box rev-parse HEAD
```

The submodule normally has a `.git` **file** and a detached `HEAD`. These are
supported by `Makefile.amnezia`; pass `VERSION` explicitly for release builds.
No neighboring checkout or `go.work` is required. To update, fetch a new verified
Amnezia tag, detach the submodule at it, rebuild the consumer, and commit the new
gitlink. Do not use `git submodule update --remote` as a release selection policy.

## Reproducible contract checks

The fork contains an independent consumer fixture that imports the exported
libbox API and checks an AWG configuration. It does not start a tunnel or require
an OS TUN device. These checks use fresh consumer directories:

```sh
python3 scripts/amnezia/check_consumer.py remote --ref "$tag" --expected-sha "$sha"
python3 scripts/amnezia/check_consumer.py submodule --ref "$tag" --expected-sha "$sha"
```

The remote check verifies Go's resolved commit and replacement. The submodule
check creates a consumer commit, clones it recursively from scratch, verifies
the gitlink, detached checkout and `.git` file, then builds and runs it. Both
check the AWG dependency without a second replacement. `--output <new-directory>`
keeps the fixture for inspection.

For PRs, remote replacement checks the published **head commit**, because
[Go version queries](https://go.dev/ref/mod#version-queries) cannot resolve GitHub's
temporary merge refs. The submodule check and ordinary build/test jobs verify
the exact PR **merge result**. After a merge, both consumers check the exact
published branch commit; release checks use the exact tag and expected commit.

These contract checks do not replace the consuming application's own tests.
Run Android/iOS acceptance with the packaged library before a client release.
