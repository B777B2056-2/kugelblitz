"""Entry point: uv run python -m kugelblitz_eval run --dataset swebench-lite"""

import argparse
import yaml
from pathlib import Path

from .adapters.kugelblitz import KugelblitzAdapter
from .common.llm import LLMJudge
from .orchestrator import Orchestrator
from .reporters.langfuse import LangfuseReporter


def main():
    parser = argparse.ArgumentParser(description="Kugelblitz Agent Evaluation")
    sub = parser.add_subparsers(dest="command", required=True)

    p_run = sub.add_parser("run", help="Run evaluation")
    p_run.add_argument("--dataset", default="swebench-lite",
                       help="Dataset name: swebench-lite | swebench-verified")
    p_run.add_argument("--split", default="dev",
                       help="Dataset split: dev | test")
    p_run.add_argument("--agent", default="kugelblitz")
    p_run.add_argument("--cli-path", default="./cli/eval-cli",
                       help="Path to eval-cli binary")
    p_run.add_argument("--parallel", type=int, default=1)
    p_run.add_argument("--max", type=int, default=None, help="Max instances")
    p_run.add_argument("--judge-model", default="gpt-4o", help="LLM judge model")
    p_run.add_argument("--openai-key", default="", help="API key for judge")
    p_run.add_argument("--openai-base-url", default="", help="Base URL for judge")
    p_run.add_argument("--output", default="eval-report.md", help="Report output path")
    p_run.add_argument("--config", default="", help="YAML config file path")
    args = parser.parse_args()

    cfg = {}
    if args.config:
        with open(args.config, encoding="utf-8") as f:
            cfg = yaml.safe_load(f) or {}

    judge = LLMJudge(
        model=args.judge_model or cfg.get("judge_model", "gpt-4o"),
        api_key=args.openai_key or cfg.get("openai_api_key", ""),
        base_url=args.openai_base_url or cfg.get("openai_base_url", ""),
    )

    reporter = LangfuseReporter(
        public_key=cfg.get("langfuse_public_key", ""),
        secret_key=cfg.get("langfuse_secret_key", ""),
        host=cfg.get("langfuse_host", ""),
    )

    adapter = KugelblitzAdapter(cli_path=args.cli_path)
    parallel = args.parallel or cfg.get("parallel", 1)
    orch = Orchestrator(adapter, judge, reporter, parallel=parallel)

    scores = orch.run(dataset=args.dataset, split=args.split, max_instances=args.max)

    dataset_name = f"{args.dataset}-{args.split}"
    orch.generate_report(scores, dataset_name, output_path=args.output)

    avg = sum(s.total for s in scores) / len(scores) if scores else 0
    print(f"\n{'='*50}")
    print(f"Dataset: {dataset_name}  |  Instances: {len(scores)}  |  Avg: {avg:.1f}")
    print(f"Report: {args.output}")
    print(f"{'='*50}")


if __name__ == "__main__":
    main()
