"""Release gates use fixtures only; no GitHub calls or product tags."""

import json
from pathlib import Path
import sys
import unittest
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).parent))
import release_preflight as release


SHA = "a" * 40
REPOSITORY = "amnezia-vpn/amnezia-box"


def run_fixture(number=10, **updates):
    result = {
        "id": number * 100, "run_number": number, "run_attempt": 1,
        "repository": {"full_name": REPOSITORY},
        "path": ".github/workflows/amnezia-ci.yml", "event": "push",
        "head_sha": SHA, "head_branch": "dev", "status": "completed",
        "conclusion": "success",
        "html_url": f"https://github.com/{REPOSITORY}/actions/runs/{number * 100}",
    }
    result.update(updates)
    return result


def job_fixtures():
    return [
        {
            "id": index, "run_id": 1000, "head_sha": SHA, "name": name,
            "status": "completed", "conclusion": "success",
            "html_url": f"https://github.com/{REPOSITORY}/actions/runs/1000/job/{index}",
        }
        for index, name in enumerate(release.REQUIRED_JOBS, start=1)
    ]


class TagTests(unittest.TestCase):
    def test_channels(self):
        self.assertEqual(release.tag_channel("v1.260910.0"), ("master", False))
        self.assertEqual(release.tag_channel("v1.260910.0-rc.1"), ("dev", True))
        self.assertEqual(release.tag_channel("v1.240229.12-rc.2"), ("dev", True))

    def test_invalid_tag(self):
        for tag in (
            "v1.12.0", "1.260910.0", "v1.260231.0", "v1.260229.0",
            "v1.261301.0", "v1.260910.00", "v1.260910.0-rc.0",
            "v1.260910.0-beta.1", "v1.260910.0-rc.01", "v1.260910.0\n",
        ):
            with self.subTest(tag=tag), self.assertRaises(release.PreflightError):
                release.tag_channel(tag)


class CITests(unittest.TestCase):
    def select(self, runs):
        return release.select_ci_run(runs, REPOSITORY, SHA, "dev")

    def test_latest_run_wins_regardless_of_response_order(self):
        self.assertEqual(self.select([run_fixture(10), run_fixture(12), run_fixture(11)])["id"], 1200)

    def test_older_success_does_not_mask_newer_failure(self):
        for status, conclusion in (("completed", "failure"), ("in_progress", None), ("completed", "cancelled")):
            with self.subTest(status=status, conclusion=conclusion), self.assertRaises(release.PreflightError):
                self.select([run_fixture(11, status=status, conclusion=conclusion), run_fixture(10)])

    def test_other_events_branches_sha_and_workflows_are_not_evidence(self):
        for updates in (
            {"event": "pull_request"}, {"event": "workflow_dispatch"},
            {"head_branch": "master"}, {"head_sha": "b" * 40},
            {"path": ".github/workflows/other.yml"},
            {"repository": {"full_name": "other/fork"}},
        ):
            with self.subTest(updates=updates), self.assertRaises(release.PreflightError):
                self.select([run_fixture(**updates)])

    def test_required_jobs_all_success(self):
        evidence = release.validate_jobs(job_fixtures(), run_fixture(), SHA)
        self.assertEqual(set(evidence), set(release.REQUIRED_JOBS))

    def test_missing_duplicate_skipped_failed_or_wrong_commit_job(self):
        fixtures = [job_fixtures()[:-1], job_fixtures() + [job_fixtures()[0]]]
        for updates in (
            {"conclusion": "skipped"}, {"conclusion": "failure"},
            {"status": "in_progress"}, {"head_sha": "b" * 40}, {"run_id": 999},
        ):
            jobs = job_fixtures()
            jobs[0].update(updates)
            fixtures.append(jobs)
        for jobs in fixtures:
            with self.subTest(jobs=jobs), self.assertRaises(release.PreflightError):
                release.validate_jobs(jobs, run_fixture(), SHA)

    def test_rerun_start_during_verification_rejected(self):
        with patch.object(release, "api_list", side_effect=[[run_fixture()], job_fixtures()]), \
                patch.object(release, "api", return_value=run_fixture(run_attempt=2, status="in_progress")):
            with self.assertRaises(release.PreflightError):
                release.check_ci(REPOSITORY, SHA, "dev")

    def test_newer_run_appearing_during_verification_rejected(self):
        with patch.object(release, "api_list", side_effect=[
            [run_fixture()], job_fixtures(), [run_fixture(11)],
        ]), patch.object(release, "api", return_value=run_fixture()):
            with self.assertRaises(release.PreflightError):
                release.check_ci(REPOSITORY, SHA, "dev")

    def test_complete_ci_evidence(self):
        with patch.object(release, "api_list", side_effect=[
            [run_fixture()], job_fixtures(), [run_fixture()],
        ]), patch.object(release, "api", return_value=run_fixture()):
            result = release.check_ci(REPOSITORY, SHA, "dev")
        self.assertEqual(result["run_id"], 1000)
        self.assertEqual(result["run_attempt"], 1)
        self.assertEqual(len(result["jobs"]), 5)


class SourceTests(unittest.TestCase):
    def verify(self, *, published=False, head=SHA, dirty="", tip=SHA,
               local_tag="", remote_tag="", annotated=True, peeled=SHA,
               ancestry=True, shallow=False, fetched=None):
        tag = "v1.260910.0-rc.1"
        refs = {"refs/heads/dev": tip}
        if remote_tag:
            refs[f"refs/tags/{tag}"] = remote_tag
            refs[f"refs/tags/{tag}^{{}}"] = peeled

        def git_fixture(*args, allowed=(0,)):
            if args == ("rev-parse", "HEAD"):
                return head
            if args[0] == "status":
                return dirty
            if args == ("rev-parse", "--is-shallow-repository"):
                return "true" if shallow else "false"
            if args[:3] == ("rev-parse", "--verify", "--quiet"):
                return local_tag
            if args[:2] == ("cat-file", "-t"):
                return "tag" if annotated else "commit"
            if args == ("rev-parse", f"refs/tags/{tag}^{{commit}}"):
                return peeled
            if args[0] == "fetch":
                return ""
            if args == ("rev-parse", "FETCH_HEAD^{commit}"):
                return fetched or tip
            if args[0] == "merge-base":
                if not ancestry:
                    raise release.PreflightError("fixture ancestry mismatch")
                return ""
            self.fail(f"unexpected Git call: {args}")

        with patch.object(release, "git", side_effect=git_fixture), \
                patch.object(release, "remote_refs", return_value=refs):
            return release.verify_source(REPOSITORY, SHA, tag, "dev", published)

    def test_untagged_clean_tip(self):
        self.assertEqual(self.verify(), (SHA, ""))

    def test_wrong_head_dirty_shallow_or_stale_candidate_rejected(self):
        for changes in (
            {"head": "b" * 40}, {"dirty": "?? source.py"}, {"tip": "b" * 40},
            {"shallow": True}, {"ancestry": False}, {"fetched": "b" * 40},
            {"local_tag": "c" * 40}, {"remote_tag": "c" * 40},
        ):
            with self.subTest(changes=changes), self.assertRaises(release.PreflightError):
                self.verify(**changes)

    def test_published_tag_allows_branch_advancement(self):
        self.assertEqual(self.verify(published=True, local_tag="c" * 40,
                                     remote_tag="c" * 40, tip="b" * 40),
                         ("b" * 40, "c" * 40))

    def test_published_tag_requires_annotation_exact_object_and_commit(self):
        for changes in (
            {"local_tag": ""}, {"remote_tag": "d" * 40},
            {"annotated": False}, {"peeled": "b" * 40}, {"ancestry": False},
        ):
            arguments = {"published": True, "local_tag": "c" * 40, "remote_tag": "c" * 40}
            arguments.update(changes)
            with self.subTest(changes=changes), self.assertRaises(release.PreflightError):
                self.verify(**arguments)


class FinalGateTests(unittest.TestCase):
    def preflight(self, *, tag_object="c" * 40, remote_object="c" * 40,
                  remote_commit=SHA, tip=SHA, head=SHA, dirty="", local_tag="c" * 40):
        args = SimpleNamespace(repository=REPOSITORY, sha=SHA,
                               tag="v1.260910.0-rc.1", published_tag=True)
        refs = {"refs/heads/dev": tip, f"refs/tags/{args.tag}": remote_object,
                f"refs/tags/{args.tag}^{{}}": remote_commit}
        with patch.object(release, "verify_source", return_value=(SHA, tag_object)), \
                patch.object(release, "check_ci", return_value={}), \
                patch.object(release, "remote_refs", return_value=refs), \
                patch.object(release, "git", side_effect=[head, dirty, local_tag]):
            return release.preflight(args)

    def test_records_exact_annotated_tag_object(self):
        self.assertEqual(self.preflight()["tag_object"], "c" * 40)

    def test_source_or_tag_mutation_after_ci_rejected(self):
        for changes in (
            {"remote_object": "d" * 40}, {"remote_commit": "b" * 40},
            {"tip": "b" * 40}, {"head": "b" * 40},
            {"dirty": " M source.py"}, {"local_tag": "d" * 40},
        ):
            with self.subTest(changes=changes), self.assertRaises(release.PreflightError):
                self.preflight(**changes)

    def test_full_immutable_sha_required_before_git_calls(self):
        args = SimpleNamespace(repository=REPOSITORY, sha="abc1234",
                               tag="v1.260910.0-rc.1", published_tag=False)
        with patch.object(release, "verify_source") as source:
            with self.assertRaises(release.PreflightError):
                release.preflight(args)
            source.assert_not_called()


class PaginationTests(unittest.TestCase):
    def test_multiple_pages_combined(self):
        pages = [{"total_count": 2, "jobs": [{"id": 1}]},
                 {"total_count": 2, "jobs": [{"id": 2}]}]
        with patch.object(release, "command", return_value=json.dumps(pages)) as command:
            self.assertEqual(release.api_list("endpoint", "jobs"), [{"id": 1}, {"id": 2}])
            self.assertIn("--paginate", command.call_args.args[0])
            self.assertIn("github.com", command.call_args.args[0])

    def test_truncated_or_malformed_response_rejected(self):
        for response in ("not json", "[]", '[{"total_count": 3, "jobs": []}]', '[{}]'):
            with self.subTest(response=response), \
                    patch.object(release, "command", return_value=response), \
                    self.assertRaises(release.PreflightError):
                release.api_list("endpoint", "jobs")


if __name__ == "__main__":
    unittest.main()
