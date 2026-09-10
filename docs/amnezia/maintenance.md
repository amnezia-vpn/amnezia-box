# Amnezia maintenance

This fork uses `master` as its stable line and `dev` as its integration line.
The upstream baseline is SagerNet/sing-box v1.12.12
(`54ed58499d7063136ed52dabf87d179d252425d0`), with Amnezia's AWG changes merged
on top. The existing `dev-next` branch is a separate upstream development line;
do not merge it into `dev` as part of routine fork cleanup.

Amnezia CI runs on pull requests and pushes to `master`/`dev` regardless of the
default branch. An administrator should select `master` as the default branch
and configure the protections below. Inherited publishing jobs are restricted
to the SagerNet repository.

## Upstream synchronization

Keep SagerNet branches as clean remote-tracking refs under a dedicated `upstream` remote. Fetch branches without importing upstream tags into the fork's shared tag namespace. When a release is selected, fetch only that tag into a remote ref and resolve it to a commit:

```sh
git fetch --no-tags upstream
git fetch --no-tags upstream 'refs/tags/<upstream-tag>:refs/remotes/upstream/tags/<upstream-tag>'
git rev-parse 'refs/remotes/upstream/tags/<upstream-tag>^{}'
```

Create an upstream sync branch from `origin/dev`, then merge the selected commit with a regular merge commit. Resolve conflicts and pass CI and consumer checks before merging the sync branch into `dev`. Promote tested `dev` changes to `master`, then merge the resulting `master` back into `dev`.

Record the old and new upstream tags/SHAs in the sync PR, together with resolved
conflicts and validation. Preserve the upstream commits with a merge; do not
rebuild the patch series from scratch or take whole conflicting files from one
side. Review these shared files especially carefully:

- `go.mod`/`go.sum` and the separate `test/` module;
- AWG registration, options, transport and protocol code;
- `cmd/internal/build_libbox/main.go`, whose extra version/tag/output hooks
  preserve the upstream mobile profiles;
- `README.md` and every `.github/workflows/` file.

New upstream workflow files can bypass guards in existing files. Inspect their
triggers, permissions, secrets and publishing destinations before merging a sync
PR or switching the default branch. Keep Amnezia-specific commands and docs in
`Makefile.amnezia`, `scripts/amnezia/` and `docs/amnezia/` to limit shared edits.

Do not rebase, reset, or force-push published `master` or `dev` history. Never use the inherited `make update` target for fork maintenance: it performs destructive reset and clean operations. Publish only a specifically selected Amnezia tag, never all fetched tags.

Before creating a source tag, verify the exact commit, its upstream baseline, the intended version, and all required CI and consumer results. Run this preflight before the tag is published because a later tag workflow cannot retract a version already observed by Go module infrastructure.

## Consumer contracts

The canonical module path remains `github.com/sagernet/sing-box`. Consumers may use either of these designs:

- Keep the upstream module requirement and add a remote `replace` to an immutable Amnezia tag.
- Add this repository under `third_party/amnezia-box` as a Git submodule and use `replace github.com/sagernet/sing-box => ./third_party/amnezia-box`.

For the submodule design, `.gitmodules`, the pinned gitlink, and submodule checkout belong in the consumer repository. Consumers should build with `GOWORK=off` and must not depend on adjacent local checkouts.

See [consumer checks](consumer.md), [build profiles](build.md), and the
[release procedure](release.md).

## CI and repository protection

Require these stable checks on `master` and `dev`:

- `Module consistency and tests`
- `AWG tests and race`
- `Supported builds`
- `consumer-remote`
- `consumer-submodule`

`Supported builds` requires the CLI, Android and Apple packaging jobs to succeed.
Module checks include consistency and compilation of the separate `test/`
module; they do not run its Docker integration suite. Run that suite explicitly
inside `test/` when validating a change that needs its services.

Use a branch ruleset that requires PRs and these checks on current merge results,
prevents deletion and force pushes, and resolves review conversations. Require
one approval when another maintainer can review; for a sole maintainer, keep the
PR and CI requirement without creating an impossible self-approval requirement.
Do not require linear history: regular merges preserve upstream and PR ancestry.

Protect `v1.*` tags against updates and deletion. A separate creation restriction
should allow only release maintainers; do not give its bypass actors a bypass
of tag immutability. These settings require repository administration rights.

Before deleting an old branch, verify that its tip is an ancestor of `dev` and
`master`, no open PR uses it as a head or base, and no consumer references the
branch. Keep `dev-next` until a separate decision about that line.
