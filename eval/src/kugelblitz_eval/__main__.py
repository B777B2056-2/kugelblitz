"""Entry point: uv run python -m kugelblitz_eval run --dataset ... --agent kugelblitz"""

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

    # ── run ──
    p_run = sub.add_parser("run", help="Run evaluation on a dataset")
    p_run.add_argument("--dataset", required=True, help="Path to SWE-bench JSONL")
    p_run.add_argument("--agent", default="kugelblitz", help="Agent adapter to use")
    p_run.add_argument("--cli-path", default="./cli/eval-cli",
                       help="Path to eval-cli binary (kugelblitz only)")
    p_run.add_argument("--parallel", type=int, default=1, help="Parallel instances")
    p_run.add_argument("--max", type=int, default=None, help="Max instances to evaluate")
    p_run.add_argument("--judge-model", default="gpt-4o", help="LLM judge model")
    p_run.add_argument("--openai-key", default="", help="OpenAI API key for judge")
    p_run.add_argument("--openai-base-url", default="", help="OpenAI base URL")
    p_run.add_argument("--langfuse-host", default="", help="Langfuse host")
    p_run.add_argument("--langfuse-pk", default="", help="Langfuse public key")
    p_run.add_argument("--langfuse-sk", default="", help="Langfuse secret key")
    p_run.add_argument("--output", default="eval-report.md", help="Report output path")
    p_run.add_argument("--config", default="", help="YAML config file path")
    args = parser.parse_args()

    # Load YAML config if provided (CLI flags override)
    cfg = {}
    if args.config:
        with open(args.config) as f:
            cfg = yaml.safe_load(f) or {}

    judge = LLMJudge(
        model=args.judge_model or cfg.get("judge_model", "gpt-4o"),
        api_key=args.openai_key or cfg.get("openai_api_key", ""),
        base_url=args.openai_base_url or cfg.get("openai_base_url", ""),
    )

    reporter = LangfuseReporter(
        public_key=args.langfuse_pk or cfg.get("langfuse_public_key", ""),
        secret_key=args.langfuse_sk or cfg.get("langfuse_secret_key", ""),
        host=args.langfuse_host or cfg.get("langfuse_host", ""),
    )

    adapter = KugelblitzAdapter(cli_path=args.cli_path)

    parallel = args.parallel or cfg.get("parallel", 1)
    orch = Orchestrator(adapter, judge, reporter, parallel=parallel)

    dataset_name = Path(args.dataset).stem
    scores = orch.run(args.dataset, max_instances=args.max)
    orch.generate_report(scores, dataset_name, output_path=args.output)

    # Print quick summary
    avg = sum(s.total for s in scores) / len(scores) if scores else 0
    print(f"\n{'='*50}")
    print(f"Dataset: {dataset_name}  |  Instances: {len(scores)}  |  Avg: {avg:.1f}")
    print(f"Report: {args.output}")
    print(f"{'='*50}")


if __name__ == "__main__":
    main()
