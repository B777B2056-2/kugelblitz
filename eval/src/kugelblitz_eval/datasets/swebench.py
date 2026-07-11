"""SWE-bench dataset — loads directly from Hugging Face via pandas.

Supports SWE-bench_Lite (300 instances, dev+test splits) and
SWE-bench_Verified (500 instances, test split).
"""

import json
from dataclasses import dataclass, field

import pandas as pd


# Hugging Face paths
DATASETS = {
    "swebench-lite": "hf://datasets/SWE-bench/SWE-bench_Lite",
    "swebench-verified": "hf://datasets/princeton-nlp/SWE-bench_Verified",
}


@dataclass
class SWEBenchInstance:
    instance_id: str
    repo: str
    base_commit: str
    issue: str
    hints: str = ""
    fail_to_pass: list[str] = field(default_factory=list)
    pass_to_pass: list[str] = field(default_factory=list)


def load_instances(dataset: str = "swebench-lite", split: str = "dev",
                   max_count: int | None = None) -> list[SWEBenchInstance]:
    """Load SWE-bench instances directly from Hugging Face parquet files.

    Args:
        dataset: 'swebench-lite' or 'swebench-verified'
        split: 'dev' or 'test' (swebench-lite has both, verified only 'test')
        max_count: limit number of instances (None = all)

    Requires: huggingface-cli login (if dataset is gated)
    """
    path = DATASETS.get(dataset)
    if not path:
        raise ValueError(f"Unknown dataset: {dataset}. Use: {list(DATASETS.keys())}")

    splits_map = {"test": "test-00000-of-00001.parquet"}
    if dataset == "swebench-lite":
        splits_map["dev"] = "dev-00000-of-00001.parquet"

    file_name = splits_map.get(split)
    if not file_name:
        raise ValueError(f"Unknown split: {split} for {dataset}")

    url = f"{path}/data/{file_name}"
    df = pd.read_parquet(url)

    if max_count:
        df = df.head(max_count)

    instances = []
    for _, row in df.iterrows():
        instances.append(SWEBenchInstance(
            instance_id=row["instance_id"],
            repo=row["repo"],
            base_commit=row["base_commit"],
            issue=row.get("problem_statement", row.get("issue", "")),
            hints=row.get("hints_text", row.get("hints", "")),
            fail_to_pass=_parse_json_list(row.get("FAIL_TO_PASS")),
            pass_to_pass=_parse_json_list(row.get("PASS_TO_PASS")),
        ))
    return instances


def build_goal(instance: SWEBenchInstance) -> str:
    parts = [
        f"Fix the following issue in repository {instance.repo}:\n\n{instance.issue}",
    ]
    if instance.fail_to_pass:
        ftp = ", ".join(instance.fail_to_pass)
        parts.append(
            f"\nThe failing test(s): {ftp}. "
            "After fixing, run them to verify they pass."
        )
    if instance.hints:
        parts.append(f"\nHints: {instance.hints}")
    return "\n".join(parts)


def _parse_json_list(val) -> list[str]:
    """FAIL_TO_PASS / PASS_TO_PASS can be JSON strings or already parsed."""
    if val is None:
        return []
    if isinstance(val, list):
        return val
    try:
        return json.loads(str(val))
    except (json.JSONDecodeError, TypeError):
        return []
