"""SWE-Bench dataset loader + Docker sandbox management.

Each SWE-Bench instance has:
  instance_id, repo, base_commit, issue (text), hints,
  FAIL_TO_PASS (tests that must pass after fix),
  PASS_TO_PASS (tests that must still pass after fix).

Workflow:
  1. Load JSONL → list[SWEBenchInstance]
  2. For each instance: docker.create(repo, base_commit) → Sandbox
  3. Agent runs inside sandbox (via eval-cli --workdir <sandbox-path>)
  4. Collect patch from sandbox, run tests, score
"""

import json
import subprocess
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class SWEBenchInstance:
    instance_id: str
    repo: str
    base_commit: str
    issue: str
    hints: str = ""
    fail_to_pass: list[str] = field(default_factory=list)
    pass_to_pass: list[str] = field(default_factory=list)


def load_instances(path: str | Path, max_count: int | None = None) -> list[SWEBenchInstance]:
    """Load SWE-bench instances from a JSONL file."""
    instances = []
    with open(path) as f:
        for line in f:
            if max_count and len(instances) >= max_count:
                break
            raw = json.loads(line)
            instances.append(SWEBenchInstance(
                instance_id=raw["instance_id"],
                repo=raw["repo"],
                base_commit=raw["base_commit"],
                issue=raw.get("problem_statement", raw.get("issue", "")),
                hints=raw.get("hints", ""),
                fail_to_pass=json.loads(raw.get("FAIL_TO_PASS", "[]")),
                pass_to_pass=json.loads(raw.get("PASS_TO_PASS", "[]")),
            ))
    return instances


class DockerSandbox:
    """Manages a per-instance Docker container with the target repo checked out."""

    def __init__(self, instance: SWEBenchInstance, image: str = "ubuntu:22.04"):
        self.instance = instance
        self.image = image
        self.container_id: str | None = None
        self._workdir = "/workspace"

    def create(self) -> str:
        """Start container, clone repo, checkout base_commit. Returns host workdir path."""
        # docker run -d --rm -v ... image sleep infinity
        # docker exec ... git clone ... && git checkout base_commit
        # Return host-mounted path for eval-cli --workdir
        raise NotImplementedError("Phase 2")

    def exec_test(self, test_spec: str) -> bool:
        """Run a single test in the sandbox. Return True if passed."""
        raise NotImplementedError("Phase 2")

    def cleanup(self):
        """Stop and remove container."""
        if self.container_id:
            subprocess.run(["docker", "rm", "-f", self.container_id], capture_output=True)

    @property
    def workdir(self) -> str:
        return self._workdir
