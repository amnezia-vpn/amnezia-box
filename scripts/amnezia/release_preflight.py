#!/usr/bin/env python3
"""Verify an immutable Amnezia source candidate before tagging or publishing."""

import argparse
from datetime import date
import json
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlencode


UPSTREAM_BASELINE = "54ed58499d7063136ed52dabf87d179d252425d0"
AWG_VERSION = "v3.1.20260828"
GO_VERSION = "1.25.5"
WORKFLOW = "amnezia-ci.yml"
REQUIRED_JOBS = (
    "Module consistency and tests", "AWG tests and race", "Supported builds",
    "consumer-remote", "consumer-submodule",
)


class PreflightError(RuntimeError):
    """The candidate has insufficient or conflicting release evidence."""


def require(condition, message):
    if not condition:
        raise PreflightError(message)


def command(args, allowed=(0,)):
    try:
        result = subprocess.run(args, text=True, capture_output=True, timeout=180)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise PreflightError(f"cannot complete {args[0]} command") from error
    # Do not echo subprocess stderr: authentication failures can contain private
    # URLs or credentials. The caller supplies the context for each gate.
    require(result.returncode in allowed, f"{args[0]} command failed (exit {result.returncode})")
    return result.stdout.strip()


def git(*args, allowed=(0,)):
    return command(["git", *args], allowed=allowed)


def api(endpoint):
    output = command(["gh", "api", "--hostname", "github.com", "--method", "GET", endpoint])
    try:
        return json.loads(output)
    except json.JSONDecodeError as error:
        raise PreflightError("invalid GitHub API response") from error


def api_list(endpoint, key):
    output = command(["gh", "api", "--hostname", "github.com", "--method", "GET", "--paginate", "--slurp", endpoint])
    try:
        pages = json.loads(output)
        items = [item for page in pages for item in page[key]]
        require(bool(pages), "empty GitHub API pagination response")
        require(pages[0].get("total_count", len(items)) <= len(items), "incomplete GitHub API evidence")
        return items
    except (TypeError, KeyError, json.JSONDecodeError) as error:
        raise PreflightError("invalid GitHub API list response") from error


def tag_channel(tag):
    match = re.fullmatch(r"v1\.([0-9]{6})\.(0|[1-9][0-9]*)(?:-rc\.([1-9][0-9]*))?", tag)
    require(match is not None, "tag must be v1.YYMMDD.N or v1.YYMMDD.N-rc.K (K starts at 1)")
    stamp = match[1]
    try:
        date(2000 + int(stamp[:2]), int(stamp[2:4]), int(stamp[4:6]))
    except ValueError as error:
        raise PreflightError("tag contains an invalid calendar date") from error
    prerelease = match[3] is not None
    return ("dev" if prerelease else "master"), prerelease


def select_ci_run(runs, repository, sha, channel):
    matches = [run for run in runs if (
        run.get("repository", {}).get("full_name", "").lower() == repository.lower()
        and run.get("path", "").split("@")[0] == f".github/workflows/{WORKFLOW}"
        and run.get("event") == "push"
        and run.get("head_sha") == sha
        and run.get("head_branch") == channel
    )]
    require(bool(matches), f"no Amnezia CI push run for exact {channel} SHA {sha}")
    require(all(isinstance(run.get("run_number"), int) and isinstance(run.get("id"), int) for run in matches),
            "CI run identity is missing")
    # Never filter for success before choosing the newest run. A failed or
    # queued newer run invalidates an older successful result for the same SHA.
    latest = max(matches, key=lambda run: (run["run_number"], run["id"]))
    require(latest.get("status") == "completed" and latest.get("conclusion") == "success",
            f"latest Amnezia CI run {latest['id']} is not completed successfully")
    require(isinstance(latest.get("run_attempt"), int) and latest["run_attempt"] > 0,
            "CI run attempt is missing")
    return latest


def validate_jobs(jobs, run, sha):
    evidence = {}
    for name in REQUIRED_JOBS:
        matches = [job for job in jobs if job.get("name") == name]
        require(len(matches) == 1, f"required CI job missing or ambiguous: {name}")
        job = matches[0]
        require(job.get("run_id") == run["id"] and job.get("head_sha") == sha,
                f"required CI job has a different source: {name}")
        require(job.get("status") == "completed" and job.get("conclusion") == "success",
                f"required CI job did not succeed: {name}")
        require(isinstance(job.get("html_url"), str), f"required CI job URL missing: {name}")
        evidence[name] = job["html_url"]
    return evidence


def check_ci(repository, sha, channel):
    query = urlencode({"event": "push", "branch": channel, "head_sha": sha, "per_page": 100})
    endpoint = f"repos/{repository}/actions/workflows/{WORKFLOW}/runs?{query}"
    run = select_ci_run(api_list(endpoint, "workflow_runs"), repository, sha, channel)
    jobs = api_list(f"repos/{repository}/actions/runs/{run['id']}/jobs?filter=latest&per_page=100", "jobs")
    evidence = validate_jobs(jobs, run, sha)
    current = api(f"repos/{repository}/actions/runs/{run['id']}")
    require(all(current.get(key) == run.get(key) for key in (
        "id", "head_sha", "head_branch", "event", "run_attempt", "status", "conclusion",
    )), "CI run changed during verification; rerun preflight")
    latest = select_ci_run(api_list(endpoint, "workflow_runs"), repository, sha, channel)
    require((latest["id"], latest["run_attempt"]) == (run["id"], run["run_attempt"]),
            "new CI run appeared during verification; rerun preflight")
    return {"run_id": run["id"], "run_attempt": run["run_attempt"], "url": run["html_url"], "jobs": evidence}


def remote_refs(repository, channel, tag):
    output = git("ls-remote", f"https://github.com/{repository}.git",
                 f"refs/heads/{channel}", f"refs/tags/{tag}", f"refs/tags/{tag}^{{}}")
    refs = {}
    for line in output.splitlines():
        fields = line.split()
        require(len(fields) == 2 and re.fullmatch(r"[0-9a-f]{40}", fields[0]), "invalid remote Git ref response")
        refs[fields[1]] = fields[0]
    return refs


def verify_source(repository, sha, tag, channel, published):
    require(git("rev-parse", "HEAD") == sha, "checkout HEAD does not equal the requested full SHA")
    require(not git("status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none"),
            "checkout is dirty; commit or remove pending source changes before release")
    require(git("rev-parse", "--is-shallow-repository") == "false", "release checks require a full-history checkout")
    tag_ref = f"refs/tags/{tag}"
    branch_ref = f"refs/heads/{channel}"
    local_tag = git("rev-parse", "--verify", "--quiet", tag_ref, allowed=(0, 1))
    refs = remote_refs(repository, channel, tag)
    require(branch_ref in refs, f"remote {channel} branch does not exist")
    tip = refs[branch_ref]
    if published:
        require(bool(local_tag) and refs.get(tag_ref) == local_tag, "local and published tag objects differ or are missing")
        require(git("cat-file", "-t", tag_ref) == "tag", "published release tag must be annotated")
        require(git("rev-parse", f"{tag_ref}^{{commit}}") == sha and refs.get(f"{tag_ref}^{{}}") == sha,
                "published annotated tag does not resolve to the requested SHA")
    else:
        require(not local_tag and tag_ref not in refs, "release tag already exists locally or remotely; choose a new version")
        require(tip == sha, f"candidate is not the current remote {channel} tip")

    git("fetch", "--no-tags", f"https://github.com/{repository}.git", branch_ref)
    require(git("rev-parse", "FETCH_HEAD^{commit}") == tip, "source branch advanced during fetch; rerun preflight")
    # merge-base exits nonzero when ancestry cannot be established. It must not
    # be replaced by branch names, matching trees, or a successful PR-head run.
    try:
        git("merge-base", "--is-ancestor", UPSTREAM_BASELINE, sha)
        git("merge-base", "--is-ancestor", sha, tip)
    except PreflightError as error:
        raise PreflightError("candidate is outside the required upstream/source-channel ancestry") from error
    return tip, local_tag


def preflight(args):
    require(re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", args.repository), "repository must be a GitHub owner/name")
    require(re.fullmatch(r"[0-9a-f]{40}", args.sha), "sha must be an explicit full lowercase 40-character commit hash")
    channel, prerelease = tag_channel(args.tag)
    tip, tag_object = verify_source(args.repository, args.sha, args.tag, channel, args.published_tag)
    ci = check_ci(args.repository, args.sha, channel)
    final_refs = remote_refs(args.repository, channel, args.tag)
    require(final_refs.get(f"refs/heads/{channel}") == tip, "source branch changed during verification; rerun preflight")
    if args.published_tag:
        require(final_refs.get(f"refs/tags/{args.tag}") == tag_object
                and final_refs.get(f"refs/tags/{args.tag}^{{}}") == args.sha,
                "published tag changed during verification")
    else:
        require(f"refs/tags/{args.tag}" not in final_refs, "tag appeared during verification; choose a new version")
    require(git("rev-parse", "HEAD") == args.sha
            and not git("status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none"),
            "checkout changed during verification; rerun preflight")
    require(git("rev-parse", "--verify", "--quiet", f"refs/tags/{args.tag}", allowed=(0, 1)) == tag_object,
            "local tag changed during verification; rerun preflight")
    return {
        "repository": args.repository, "sha": args.sha, "tag": args.tag, "channel": channel,
        "prerelease": prerelease, "published_tag": args.published_tag, "tag_object": tag_object,
        "upstream_baseline": UPSTREAM_BASELINE, "awg_version": AWG_VERSION,
        "go_version": GO_VERSION, "ci": ci,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", default="amnezia-vpn/amnezia-box")
    parser.add_argument("--sha", required=True, help="immutable full commit SHA")
    parser.add_argument("--tag", required=True)
    parser.add_argument("--published-tag", action="store_true", help="verify an already-published annotated tag")
    parser.add_argument("--output", type=Path, help="write JSON evidence outside the source checkout")
    args = parser.parse_args()
    try:
        if args.output:
            root = Path(git("rev-parse", "--show-toplevel")).resolve()
            require(not args.output.resolve().is_relative_to(root), "write release evidence outside the source checkout")
        evidence = preflight(args)
        result = json.dumps(evidence, indent=2) + "\n"
        if args.output:
            args.output.write_text(result)
        print(result, end="")
    except (PreflightError, OSError) as error:
        print(f"release preflight failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
