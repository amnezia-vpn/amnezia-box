# Amnezia maintenance

This fork uses `master` as its stable line and `dev` as its integration line. Both start from SagerNet/sing-box v1.12.12 (`54ed58499d7063136ed52dabf87d179d252425d0`). During bootstrap, switch the default branch to `master` before activating the new CI. The original `main` and `dev-next` branches remain available while the existing pull request stack is migrated. AWG is not integrated yet.

## Upstream synchronization

Keep SagerNet branches as clean remote-tracking refs under a dedicated `upstream` remote. Fetch branches without importing upstream tags into the fork's shared tag namespace. When a release is selected, fetch only that tag into a remote ref and resolve it to a commit:

```sh
git fetch --no-tags upstream
git fetch --no-tags upstream 'refs/tags/<upstream-tag>:refs/remotes/upstream/tags/<upstream-tag>'
git rev-parse 'refs/remotes/upstream/tags/<upstream-tag>^{}'
```

Create an upstream sync branch from `origin/dev`, then merge the selected commit with a regular merge commit. Resolve conflicts and pass CI and consumer checks before merging the sync branch into `dev`. Promote tested `dev` changes to `master`, then merge the resulting `master` back into `dev`.

Do not rebase, reset, or force-push published `master` or `dev` history. Never use the inherited `make update` target for fork maintenance: it performs destructive reset and clean operations. Publish only a specifically selected Amnezia tag, never all fetched tags.

Before creating a source tag, verify the exact commit, its upstream baseline, the intended version, and all required CI and consumer results. Run this preflight before the tag is published because a later tag workflow cannot retract a version already observed by Go module infrastructure.

## Consumer contracts

The canonical module path remains `github.com/sagernet/sing-box`. Consumers may use either of these designs:

- Keep the upstream module requirement and add a remote `replace` to an immutable Amnezia tag.
- Add this repository under `third_party/amnezia-box` as a Git submodule and use `replace github.com/sagernet/sing-box => ./third_party/amnezia-box`.

For the submodule design, `.gitmodules`, the pinned gitlink, and submodule checkout belong in the consumer repository. Consumers should build with `GOWORK=off` and must not depend on adjacent local checkouts.

The current CI covers the v1.12.12 baseline module, vet, race-test, and CLI build profile. AWG-specific checks, remote-replace checks, submodule checks, source-archive checks, and release-tag checks will be added with the corresponding product stages.
