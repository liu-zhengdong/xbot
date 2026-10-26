#!/usr/bin/env python3
"""Exercise dev build metadata with a fake compiler and isolated state."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class DevBuildTest(unittest.TestCase):
    def test_tip_tracks_successful_builds(self):
        script = Path(os.environ.get("XBOT_DEV_SCRIPT", Path(__file__).with_name("xbot-dev")))
        # Load the actual build function without invoking start/restart or touching
        # the real dev instance. All commands below use this isolated directory.
        definitions = script.read_text().split('\ncmd="${1:-}"', 1)[0]
        with tempfile.TemporaryDirectory(prefix="xbot-dev-test-") as temp:
            root = Path(temp)
            fakebin = root / "fakebin"
            fakebin.mkdir()
            (fakebin / "git").write_text(
                '#!/bin/bash\ncase "$*" in\n'
                '*"rev-parse --short HEAD") printf "%s\\n" "$TEST_BUILD_HEAD";;\n'
                '*"status --porcelain") exit 0;;\n'
                '*) exit 2;;\nesac\n'
            )
            (fakebin / "go").write_text(
                '#!/bin/bash\n[ "$TEST_BUILD_FAIL" = 0 ] || exit 1\n'
                '[ "$1" = build ] && [ "$2" = -ldflags ] && [ "$4" = -o ] || exit 2\n'
                '[ "$3" = "-X xbot/internal/version.Tip=$TEST_BUILD_HEAD" ] || exit 3\n'
                'printf "%s\\n" "${3##*=}" > "$5"\n'
            )
            for executable in fakebin.iterdir():
                executable.chmod(0o755)
            harness = root / "build.sh"
            harness.write_text(
                definitions
                + '\nSTATE="$TEST_BUILD_STATE"\nBIN="$STATE/bin/xbot"\n'
                + 'INFO="$STATE/build-info"\nbuild\n'
            )
            state = root / "state"
            (state / "bin").mkdir(parents=True)
            tip = state / "bin/xbot.tip"
            tip.write_text("aab566c\n")
            env = dict(os.environ, PATH=f"{fakebin}:{os.environ['PATH']}",
                       TEST_BUILD_STATE=str(state), TEST_BUILD_FAIL="0")
            for head in ("c91e7dd", "123abcd"):
                env["TEST_BUILD_HEAD"] = head
                subprocess.run(["bash", str(harness)], env=env, check=True,
                               capture_output=True, text=True)
                self.assertEqual(tip.read_text(), head + "\n")
                self.assertEqual((state / "bin/xbot").read_text(), head + "\n")
                self.assertIn("commit=" + head + "\n", (state / "build-info").read_text())

            # A failed compiler must not advertise an unbuilt commit.
            info = (state / "build-info").read_text()
            env.update(TEST_BUILD_HEAD="badbeef", TEST_BUILD_FAIL="1")
            failed = subprocess.run(["bash", str(harness)], env=env,
                                    capture_output=True, text=True)
            self.assertNotEqual(failed.returncode, 0)
            self.assertEqual(tip.read_text(), "123abcd\n")
            self.assertEqual((state / "build-info").read_text(), info)
            self.assertEqual((state / "bin/xbot").read_text(), "123abcd\n")


if __name__ == "__main__":
    unittest.main()
