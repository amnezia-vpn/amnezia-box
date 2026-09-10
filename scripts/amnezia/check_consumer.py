#!/usr/bin/env python3
"""Build an independent consumer of a published Go module or Git submodule."""

import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile


MODULE = "github.com/sagernet/sing-box"
AWG_MODULE = "github.com/amnezia-vpn/amneziawg-go/v3"
TAGS = "with_gvisor,with_awg,with_conntrack"


def run(args, cwd, env, *, check=True):
    result = subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True)
    if check and result.returncode:
        raise RuntimeError(f"{args[0]} failed ({result.returncode}):\n{result.stdout}{result.stderr}")
    return result


def git(args, cwd, env):
    return run(["git", *args], cwd, env).stdout.strip()


def write_consumer(directory, replacement):
    directory.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(Path(__file__).with_name("testdata") / "consumer.go", directory / "main.go")
    (directory / "go.mod").write_text(
        f"module example.com/amnezia-consumer\n\ngo 1.25.0\n\n"
        f"require {MODULE} v0.0.0\n\nreplace {MODULE} => {replacement}\n"
    )


def check_build(directory, env, replacement_path, version=None):
    run(["go", "mod", "tidy"], directory, env)
    run(["go", "mod", "verify"], directory, env)
    module = json.loads(run(["go", "list", "-m", "-json", MODULE], directory, env).stdout)
    replacement = module.get("Replace", {})
    if replacement.get("Path") != replacement_path or (version and replacement.get("Version") != version):
        raise RuntimeError(f"unexpected sing-box replacement: {replacement}")
    if version is None and Path(replacement["Dir"]).resolve() != (directory / "third_party/amnezia-box").resolve():
        raise RuntimeError("local replacement escaped the cloned consumer")
    awg = json.loads(run(["go", "list", "-m", "-json", AWG_MODULE], directory, env).stdout)
    if awg.get("Replace"):
        raise RuntimeError("consumer requires an unexpected separate AWG replacement")
    binary = directory / "consumer"
    run(["go", "build", "-trimpath", "-tags", TAGS, "-o", str(binary), "."], directory, env)
    result = run([str(binary)], directory, env)
    print(result.stdout.strip())
    print(f"AWG dependency: {awg['Path']} {awg['Version']} (no replacement)")


def remote_consumer(args, workspace, env):
    fork = f"github.com/{args.repository}"
    info = json.loads(run(["go", "list", "-m", "-json", f"{fork}@{args.ref}"], workspace, env).stdout)
    if info.get("Origin", {}).get("Hash") != args.expected_sha:
        raise RuntimeError(f"Go resolved an unexpected source commit: {info.get('Origin')}")
    version = info["Version"]
    consumer = workspace / "remote"
    write_consumer(consumer, f"{fork} {version}")
    check_build(consumer, env, fork, version)
    print(f"remote consumer passed: {version} -> {args.expected_sha}")


def submodule_consumer(args, workspace, env):
    seed = workspace / "seed"
    write_consumer(seed, "./third_party/amnezia-box")
    git(["init", "-q"], seed, env)
    url = f"https://github.com/{args.repository}.git"
    git(["submodule", "add", "--depth", "1", url, "third_party/amnezia-box"], seed, env)
    submodule = seed / "third_party/amnezia-box"
    git(["fetch", "--depth", "1", "origin", args.ref], submodule, env)
    resolved = git(["rev-parse", "FETCH_HEAD^{commit}"], submodule, env)
    if resolved != args.expected_sha:
        raise RuntimeError(f"Git resolved {resolved}, expected {args.expected_sha}")
    git(["checkout", "--detach", resolved], submodule, env)
    git(["add", "main.go", "go.mod", ".gitmodules", "third_party/amnezia-box"], seed, env)
    git([
        "-c", "user.name=FrogRocky",
        "-c", "user.email=269552296+FrogRocky@users.noreply.github.com",
        "-c", "commit.gpgsign=false", "commit", "-qm", "Pin consumer fixture",
    ], seed, env)

    consumer = workspace / "submodule"
    # Only the generated consumer is local. Its submodule is fetched afresh
    # from the public repository; no adjacent source checkout is referenced.
    git(["clone", "--no-local", "--recurse-submodules", str(seed), str(consumer)], workspace, env)
    submodule = consumer / "third_party/amnezia-box"
    if not (submodule / ".git").is_file():
        raise RuntimeError("expected the real submodule .git file")
    if git(["rev-parse", "HEAD"], submodule, env) != resolved:
        raise RuntimeError("recursive clone changed the pinned submodule commit")
    if run(["git", "symbolic-ref", "-q", "HEAD"], submodule, env, check=False).returncode != 1:
        raise RuntimeError("submodule checkout is not detached")
    if not git(["ls-files", "--stage", "third_party/amnezia-box"], consumer, env).startswith(f"160000 {resolved} "):
        raise RuntimeError("consumer does not contain the expected gitlink")
    version = run(["make", "-f", "Makefile.amnezia", "version", "VERSION=consumer-check"], submodule, env)
    if version.stdout.strip() != "consumer-check":
        raise RuntimeError("explicit version did not work in the submodule checkout")
    check_build(consumer, env, "./third_party/amnezia-box")
    print(f"submodule consumer passed: detached .git-file checkout at {resolved}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=["remote", "submodule"])
    parser.add_argument("--repository", default="amnezia-vpn/amnezia-box")
    parser.add_argument("--ref", required=True)
    parser.add_argument("--expected-sha", required=True)
    parser.add_argument("--output", type=Path, help="keep the fixture in this new directory")
    args = parser.parse_args()
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repository):
        parser.error("repository must be a GitHub owner/name")
    if not re.fullmatch(r"[0-9a-f]{40}", args.expected_sha):
        parser.error("expected-sha must be a full commit hash")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_./-]*", args.ref):
        parser.error("ref must be a commit, branch, tag, or pull-request ref")
    env = dict(os.environ, GOWORK="off", GOTOOLCHAIN="local")
    # Cross-compilation settings from a caller must not prevent executing the
    # consumer on the current host.
    for name in ("GOOS", "GOARCH", "GOARM", "GOARM64", "GOAMD64"):
        env.pop(name, None)
    if args.output:
        workspace = args.output.resolve()
        workspace.mkdir(parents=True, exist_ok=False)
        (remote_consumer if args.mode == "remote" else submodule_consumer)(args, workspace, env)
    else:
        with tempfile.TemporaryDirectory(prefix="amnezia-consumer-") as directory:
            (remote_consumer if args.mode == "remote" else submodule_consumer)(args, Path(directory), env)


if __name__ == "__main__":
    main()
