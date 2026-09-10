import contextlib
import io
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import check_consumer as consumer


class ConsumerPathTests(unittest.TestCase):
    def test_local_replacement_through_symlink(self):
        # macOS exposes /private/var/folders through /var/folders. A fresh
        # consumer in that temporary directory must accept Go's canonical path.
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "real/third_party/amnezia-box"
            source.mkdir(parents=True)
            alias = root / "alias"
            alias.symlink_to(root / "real", target_is_directory=True)

            def go_result(args, cwd, env):
                output = ""
                if args[:4] == ["go", "list", "-m", "-json"]:
                    if args[4] == consumer.MODULE:
                        output = json.dumps({"Replace": {
                            "Path": "./third_party/amnezia-box", "Dir": str(source.resolve())
                        }})
                    else:
                        output = json.dumps({"Path": consumer.AWG_MODULE, "Version": "v3.1.20260828"})
                return subprocess.CompletedProcess(args, 0, output, "")

            with patch.object(consumer, "run", side_effect=go_result), contextlib.redirect_stdout(io.StringIO()):
                consumer.check_build(alias, {}, "./third_party/amnezia-box")

    def test_local_replacement_outside_consumer_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            replacement = {"Replace": {"Path": "./third_party/amnezia-box", "Dir": str(root / "neighbor")}}
            result = subprocess.CompletedProcess([], 0, json.dumps(replacement), "")
            with patch.object(consumer, "run", return_value=result):
                with self.assertRaisesRegex(RuntimeError, "escaped"):
                    consumer.check_build(root, {}, "./third_party/amnezia-box")
