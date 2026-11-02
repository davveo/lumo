"""The Go wrapper must not create cache files inside a checkout."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest

PROJECT = Path(__file__).resolve().parents[1]


class GoEnvironmentTests(unittest.TestCase):
    def test_project_local_paths_are_replaced_and_external_paths_are_preserved(self):
        with tempfile.TemporaryDirectory() as temporary:
            tool = Path(temporary) / "go-probe"
            tool.write_text('#!/bin/sh\nprintf "%s\\n" "$GOPATH" "$GOMODCACHE" "$GOCACHE"\n')
            tool.chmod(0o755)
            env = dict(
                os.environ,
                LUMO_GO=str(tool),
                GOPATH=str(PROJECT),
                GOMODCACHE=str(PROJECT / "pkg/mod"),
                GOCACHE=str(PROJECT / "build-cache"),
            )
            paths = subprocess.check_output(
                ["sh", str(PROJECT / "scripts/go.sh"), "env"], env=env, text=True
            ).splitlines()
            for value in paths:
                self.assertTrue(Path(value).is_absolute())
                self.assertFalse(Path(value).is_relative_to(PROJECT))
            env.update(
                GOPATH=temporary, GOMODCACHE=str(Path(temporary) / "modules"), GOCACHE=str(Path(temporary) / "build")
            )
            preserved = subprocess.check_output(
                ["sh", str(PROJECT / "scripts/go.sh"), "env"], env=env, text=True
            ).splitlines()
            self.assertEqual(preserved, [env["GOPATH"], env["GOMODCACHE"], env["GOCACHE"]])
