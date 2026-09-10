# Amnezia source releases

Release tags are immutable. Use `v1.YYMMDD.N` for a stable release and
`v1.YYMMDD.N-rc.K` for a release candidate. The date must exist in the calendar,
`N` starts at 0, and `K` starts at 1. RCs come from `dev`; stable releases come
from `master`. The first planned pair is `v1.260910.0-rc.1` and `v1.260910.0`.

The release workflow publishes source release notes and GitHub's source archives.
It does not publish binaries, AAR/XCFramework packages, containers, Fury packages,
or app-store releases. Upstream tags and release workflows are not Amnezia's
release process.

## Before creating a tag

Use a full-history, clean checkout and the authenticated `gh` CLI. Read access to
repository contents and Actions is sufficient for preflight. Select and record
the complete immutable candidate commit SHA before running checks. A branch name,
abbreviated hash, local build, or a successful PR-head run is insufficient.

The preflight command verifies all of the following:

- The checkout is clean, including untracked files and submodule changes, and
  `HEAD` equals the requested full SHA.
- The candidate equals the current remote channel tip and descends from upstream
  baseline `54ed58499d7063136ed52dabf87d179d252425d0`.
- The requested tag is absent locally and remotely.
- The latest **Amnezia CI push run** for that exact SHA and channel is completed
  successfully. A newer queued, cancelled, or failed run blocks release even if
  an older run passed.
- All five required jobs actually succeeded: `Module consistency and tests`,
  `AWG tests and race`, `Supported builds`, `consumer-remote`, and
  `consumer-submodule`. Missing or skipped jobs do not count as success.

The command uses the canonical GitHub repository URL and `gh api`, fetches the
source branch without tags, and writes no remote refs. Its optional JSON evidence
file must be outside the checkout. Keep that evidence with the release record.

## Release candidate from dev

Fetch the channel without importing upstream tags, inspect its source/CI, and
record the selected SHA. In the commands below, replace `FULL_REVIEWED_DEV_SHA`
with the actual 40-character commit hash; do not leave a moving branch expression
in its place.

```sh
git fetch --no-tags origin dev
git log -1 --format='%H %s' FETCH_HEAD
readonly RC_SHA=FULL_REVIEWED_DEV_SHA
readonly RC_TAG=v1.260910.0-rc.1
git checkout --detach "$RC_SHA"

python3 -B scripts/amnezia/release_preflight.py \
  --sha "$RC_SHA" --tag "$RC_TAG" \
  --output "/tmp/${RC_TAG}-preflight.json"
```

Only after that command succeeds, create an annotated tag with the maintainer's
identity and push exactly its single ref through the `github-anon` SSH alias:

```sh
git -c user.name=FrogRocky \
  -c user.email=269552296+FrogRocky@users.noreply.github.com \
  -c tag.gpgSign=false tag -a "$RC_TAG" "$RC_SHA" -m "Amnezia $RC_TAG"
git push git@github-anon:amnezia-vpn/amnezia-box.git \
  "refs/tags/$RC_TAG:refs/tags/$RC_TAG"
```

Do not use `--force`, `--tags`, or move an existing released tag. If a defect is
found, make a new commit and release a new RC. If the push outcome is unclear,
inspect the remote ref before doing anything else; do not blindly recreate it.

**Preflight must run before tag creation.** A pushed tag is already visible to
Git clients and Go proxies. Failure of the later tag workflow does not hide or
retract that version.

## Verify the published tag and consumers

The tag-triggered workflow uses the workflow file from the tag ref. It does not
require `dev` or `master` to be the repository's default branch; the deferred
`dev-next` default-branch decision is separate.

Published-tag mode verifies the exact local/remote annotated tag object and
peeled commit. It permits the channel branch to advance after tagging, but the
tagged SHA must remain its ancestor and must have its own successful channel
push CI. It then checks fresh remote-module and submodule consumers at the tag,
rechecks preflight, and publishes notes with source/baseline/AWG/Go versions and
links to the actual checks.

To repeat the same read-only validation locally, fetch the published tag into a
clean validation checkout and run:

```sh
git fetch --no-tags origin "refs/tags/$RC_TAG:refs/tags/$RC_TAG"
git checkout --detach "$RC_SHA"
python3 -B scripts/amnezia/release_preflight.py \
  --published-tag --sha "$RC_SHA" --tag "$RC_TAG"
python3 -B scripts/amnezia/check_consumer.py remote \
  --ref "$RC_TAG" --expected-sha "$RC_SHA"
python3 -B scripts/amnezia/check_consumer.py submodule \
  --ref "$RC_TAG" --expected-sha "$RC_SHA"
```

Use Go 1.25.5 with `GOTOOLCHAIN=local` and `GOWORK=off` for consumer checks. These
checks resolve the remote tag and verify the exact requested SHA; they do not
use sibling source checkouts. The submodule fixture verifies a freshly cloned
gitlink, detached checkout, and `.git` file. Android/iOS application runtime
acceptance remains a separate gate before a client release.

A workflow rerun preserves an existing published release when its source marker,
tag, and prerelease status match. It refuses to overwrite a draft or conflicting
release. Notes are never silently replaced; corrections need an explicit review.

## Promote to stable on master

After the RC passes the independent source-consumer checks, open a promotion PR
from `dev` to `master`. Real consumer repositories can be validated when identified;
Android/iOS runtime acceptance is required before a client release, not this source
release. Review the merge result and merge with a merge commit. Wait for the
**post-merge push CI on the exact master merge SHA**, including both consumer
jobs. A dev CI result cannot authorize the stable tag.

Record that new 40-character SHA as `STABLE_SHA`, then perform the same preflight
and single-ref annotated-tag publication:

```sh
git fetch --no-tags origin master
git log -1 --format='%H %s' FETCH_HEAD
readonly STABLE_SHA=FULL_REVIEWED_MASTER_MERGE_SHA
readonly STABLE_TAG=v1.260910.0
git checkout --detach "$STABLE_SHA"
python3 -B scripts/amnezia/release_preflight.py \
  --sha "$STABLE_SHA" --tag "$STABLE_TAG" \
  --output "/tmp/${STABLE_TAG}-preflight.json"

git -c user.name=FrogRocky \
  -c user.email=269552296+FrogRocky@users.noreply.github.com \
  -c tag.gpgSign=false tag -a "$STABLE_TAG" "$STABLE_SHA" -m "Amnezia $STABLE_TAG"
git push git@github-anon:amnezia-vpn/amnezia-box.git \
  "refs/tags/$STABLE_TAG:refs/tags/$STABLE_TAG"

python3 -B scripts/amnezia/check_consumer.py remote \
  --ref "$STABLE_TAG" --expected-sha "$STABLE_SHA"
python3 -B scripts/amnezia/check_consumer.py submodule \
  --ref "$STABLE_TAG" --expected-sha "$STABLE_SHA"
```

The workflow passes the prerelease flag explicitly: true for `-rc.K`, false for
stable tags. Release notes include the exact source SHA, upstream baseline,
AmneziaWG `v3.1.20260828`, Go `1.25.5`, CI job URLs, and tagged consumer-check URL.

## Update an actual consumer

For a remote module replacement, update the fork version and run the consumer's
own checks with the supported tags:

```sh
go mod edit -replace=github.com/sagernet/sing-box=github.com/amnezia-vpn/amnezia-box@v1.260910.0
GOWORK=off GOTOOLCHAIN=local go mod tidy
GOWORK=off GOTOOLCHAIN=local go list -m -json github.com/sagernet/sing-box
GOWORK=off GOTOOLCHAIN=local go build -tags with_gvisor,with_awg,with_conntrack ./...
```

Verify the replacement path/version and commit the reviewed `go.mod`/`go.sum`
changes in the consumer. No separate AWG replacement is required.

For a Git submodule consumer, update the **consumer's gitlink** separately:

```sh
git -C third_party/amnezia-box fetch --no-tags origin refs/tags/v1.260910.0
git -C third_party/amnezia-box rev-parse 'FETCH_HEAD^{commit}'
# Compare the printed commit with the recorded STABLE_SHA before checkout.
git -C third_party/amnezia-box checkout --detach "$STABLE_SHA"
git add third_party/amnezia-box
git diff --cached --submodule=log
```

Keep `replace github.com/sagernet/sing-box => ./third_party/amnezia-box`, run the
consumer's build/tests, and commit the reviewed gitlink change in that consumer
repository. Validate a fresh recursive clone; existing local submodule files do
not prove that the committed pointer is correct. See [consumer integration](consumer.md).

## Preserve the release history

After a stable release, merge `master` back into `dev` through a checked PR. Apply
the same reverse merge after every master hotfix. Protect `master` and `dev` from
force pushes/deletion, require PRs and the named checks, and configure a realistic
maintainer review requirement. Tag rulesets should restrict creation to
maintainers and prohibit updates/deletion of release tags.

Do not remove completed stack branches or the old `main` until ancestry and
remaining PR/consumer references have been checked. Preserve `dev-next` until a
separate decision. Future upstream upgrades follow [maintenance](maintenance.md).
