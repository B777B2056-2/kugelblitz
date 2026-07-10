"""SWE-Bench dataset loader.

JSONL format: one JSON object per line.
  instance_id, repo, base_commit, issue (=problem_statement),
  hints, FAIL_TO_PASS (JSON array), PASS_TO_PASS (JSON array)
"""

import json
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
    """Load SWE-bench instances from JSONL."""
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
                fail_to_pass=_parse_json_str(raw.get("FAIL_TO_PASS", "[]")),
                pass_to_pass=_parse_json_str(raw.get("PASS_TO_PASS", "[]")),
            ))
    return instances


def build_goal(instance: SWEBenchInstance) -> str:
    """Build a natural-language goal for the agent from a SWE-bench instance."""
    parts = [
        f"Fix the following issue in the repository {instance.repo}:\n\n{instance.issue}",
    ]
    if instance.fail_to_pass:
        ftp = ", ".join(instance.fail_to_pass)
        parts.append(
            f"\nThe failing test(s) are: {ftp}. "
            "After fixing, run the test(s) to verify they pass."
        )
    if instance.hints:
        parts.append(f"\nHints: {instance.hints}")
    return "\n".join(parts)


def _parse_json_str(raw: str | list) -> list[str]:
    """FAIL_TO_PASS and PASS_TO_PASS can be JSON strings or already parsed."""
    if isinstance(raw, list):
        return raw
    try:
        return json.loads(raw)
    except (json.JSONDecodeError, TypeError):
        return []
