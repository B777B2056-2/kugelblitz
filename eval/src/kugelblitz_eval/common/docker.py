"""Docker sandbox — isolate agent execution per SWE-bench instance."""

import subprocess
import tempfile
from pathlib import Path


class Sandbox:
    """Per-instance Docker container with the target repo at base_commit.

    The agent runs inside this container via eval-cli --workdir <mount-path>.
    After execution, we apply the patch and run tests inside the same container.
    """

    def __init__(self, instance_id: str, repo: str, base_commit: str,
                 image: str = "python:3.11-slim"):
        self.instance_id = instance_id
        self.repo = repo
        self.base_commit = base_commit
        self.image = image
        self._name = f"kugelblitz-eval-{instance_id.replace('/', '-').replace('_', '-')}"
        self._host_dir: Path | None = None
        self._guest_dir = "/workspace"

    # ── lifecycle ──

    def start(self) -> str:
        """Create container, clone repo, checkout base_commit. Returns host workdir."""
        self._host_dir = Path(tempfile.mkdtemp(prefix="kubeval-"))

        # Pull image if needed
        subprocess.run(["docker", "pull", self.image], check=True, capture_output=True)

        # Create detached container with host dir mounted
        subprocess.run([
            "docker", "create", "--name", self._name,
            "-v", f"{self._host_dir}:{self._guest_dir}",
            "-w", self._guest_dir,
            self.image, "sleep", "infinity",
        ], check=True, capture_output=True)

        subprocess.run(["docker", "start", self._name], check=True, capture_output=True)

        # Clone repo and checkout base commit
        self._exec(["git", "clone", f"https://github.com/{self.repo}.git", "."],
                   timeout=300)
        self._exec(["git", "checkout", self.base_commit], timeout=60)
        # Install test dependencies if requirements.txt exists
        self._exec(["bash", "-c",
                    "test -f requirements.txt && pip install -r requirements.txt || true"],
                   timeout=300)
        self._exec(["bash", "-c",
                    "test -f setup.py && pip install -e . || true"],
                   timeout=300)

        return str(self._host_dir)

    def stop(self):
        """Remove container and host temp directory."""
        subprocess.run(["docker", "rm", "-f", self._name],
                       capture_output=True)
        if self._host_dir and self._host_dir.exists():
            import shutil
            shutil.rmtree(self._host_dir, ignore_errors=True)

    # ── test execution ──

    def apply_patch_and_test(self, patch_content: str,
                              fail_to_pass: list[str],
                              pass_to_pass: list[str]) -> dict:
        """Apply patch, run tests, return pass/fail counts."""
        if not patch_content.strip():
            return {"fail_to_pass_passed": 0, "fail_to_pass_total": len(fail_to_pass),
                    "pass_to_pass_passed": 0, "pass_to_pass_total": len(pass_to_pass)}

        # Write patch to temp file on host (shared via mount)
        patch_path = self._host_dir / "_kubeval_patch.diff"
        patch_path.write_text(patch_content)

        # Apply
        result = self._exec(["git", "apply", "_kubeval_patch.diff"], check=False)
        if result.returncode != 0:
            return {"fail_to_pass_passed": 0, "fail_to_pass_total": len(fail_to_pass),
                    "pass_to_pass_passed": 0, "pass_to_pass_total": len(pass_to_pass),
                    "apply_error": result.stderr[:500]}

        # Run tests
        ftp_passed = self._run_test_list(fail_to_pass)
        ptp_passed = self._run_test_list(pass_to_pass)

        return {
            "fail_to_pass_passed": ftp_passed,
            "fail_to_pass_total": len(fail_to_pass),
            "pass_to_pass_passed": ptp_passed,
            "pass_to_pass_total": len(pass_to_pass),
        }

    def _run_test_list(self, tests: list[str]) -> int:
        """Run a list of pytest tests, return count passed."""
        if not tests:
            return 0
        passed = 0
        for test in tests:
            r = self._exec(["python", "-m", "pytest", test, "-x", "--tb=short"],
                           check=False, timeout=120)
            if r.returncode == 0:
                passed += 1
        return passed

    # ── helpers ──

    def _exec(self, cmd: list[str], check: bool = True,
              timeout: int = 60) -> subprocess.CompletedProcess:
        return subprocess.run(
            ["docker", "exec", "-w", self._guest_dir, self._name] + cmd,
            capture_output=True, text=True, check=check, timeout=timeout,
        )

    # ── patch extraction from agent output ──

    @staticmethod
    def extract_patch_from_result(result) -> str:
        """Try to extract a git diff from agent's tool calls.

        Strategy:
          1. Look for shell_exec("git diff") or ("git diff --cached") in tool results
          2. Look for file_write content that looks like a patch
          3. Fallback: look for `diff --git` in final_reply
        """
        from ..adapters.base import AgentResult
        # Strategy 1: git diff from shell_exec
        for tc in result.tool_calls:
            if tc.tool_name == "shell_exec" and tc.result:
                cmd = tc.args.get("command", "")
                if "git diff" in cmd or "git format-patch" in cmd:
                    stdout = tc.result.get("stdout", "") or tc.result.get("output", "")
                    if "diff --git" in stdout:
                        return stdout

        # Strategy 2: file_write with patch content
        for tc in result.tool_calls:
            if tc.tool_name == "file_write" and tc.args:
                content = tc.args.get("content", "")
                if "diff --git" in str(content):
                    return str(content)

        # Strategy 3: embedded in final reply
        reply = result.final_reply
        if "diff --git" in reply:
            start = reply.index("diff --git")
            end = reply.rfind("```", start)
            if end > start:
                return reply[start:end]
            return reply[start:]

        return ""
