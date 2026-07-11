"""Evaluation orchestrator — ties dataset, agent, and scorers together.
No Docker: agent runs in a temp workspace, pass@k is always 0 (no test execution).
"""

from concurrent.futures import ThreadPoolExecutor, as_completed
import tempfile

from .adapters.base import AgentAdapter
from .common.llm import LLMJudge
from .datasets.swebench import SWEBenchInstance, build_goal, load_instances
from .reporters.langfuse import InstanceScores, LangfuseReporter
from .reporters.markdown import generate_report
from .scorers.e2e import E2EScore, score_e2e
from .scorers.memory import score_memory_single_turn
from .scorers.plan import score_plan
from .scorers.tools import score_tools


def grade(total: float) -> str:
    if total >= 90: return "S"
    if total >= 75: return "A"
    if total >= 60: return "B"
    if total >= 45: return "C"
    return "D"


class Orchestrator:
    def __init__(self, adapter: AgentAdapter, judge: LLMJudge,
                 reporter: LangfuseReporter, parallel: int = 1,
                 weights: tuple[float, float, float, float] = (0.30, 0.30, 0.25, 0.15)):
        self.adapter = adapter
        self.judge = judge
        self.reporter = reporter
        self.parallel = parallel
        self.w_e2e, self.w_tools, self.w_plan, self.w_memory = weights

    def run(self, dataset: str = "swebench-lite", split: str = "dev",
            max_instances: int | None = None) -> list[InstanceScores]:
        instances = load_instances(dataset=dataset, split=split, max_count=max_instances)
        if self.parallel > 1:
            return self._run_parallel(instances)
        return self._run_sequential(instances)

    def _run_sequential(self, instances: list[SWEBenchInstance]) -> list[InstanceScores]:
        results = []
        for inst in instances:
            results.append(self._eval_one(inst))
        return results

    def _run_parallel(self, instances: list[SWEBenchInstance]) -> list[InstanceScores]:
        results = []
        with ThreadPoolExecutor(max_workers=self.parallel) as pool:
            futures = {pool.submit(self._eval_one, inst): inst for inst in instances}
            for future in as_completed(futures):
                results.append(future.result())
        return results

    def _eval_one(self, inst: SWEBenchInstance) -> InstanceScores:
        workdir = tempfile.mkdtemp(prefix=f"kubeval-{inst.instance_id}-")
        goal = build_goal(inst)
        session_id = f"eval__{inst.instance_id}"

        result = self.adapter.run(session_id, goal, workdir)

        # pass@k = 0 without Docker test execution
        e2e = E2EScore(pass_k=0.0, reply_quality=_judge_reply_only(self.judge, inst, result),
                        total=0.0)
        tools = score_tools(result)
        plan = score_plan(result, self.judge)
        memory = score_memory_single_turn()

        total = (e2e.reply_quality * self.w_e2e + tools.total * self.w_tools +
                 plan.total * self.w_plan + memory.total * self.w_memory)

        scores = InstanceScores(
            instance_id=inst.instance_id,
            e2e=e2e, tools=tools, plan=plan, memory=memory,
            total=total, grade=grade(total),
        )
        self.reporter.report(scores)
        return scores

    def generate_report(self, scores: list[InstanceScores],
                        dataset_name: str, output_path: str = "eval-report.md") -> str:
        return generate_report(scores, dataset_name, self.adapter.name(), output_path)


def _judge_reply_only(judge: LLMJudge, inst: SWEBenchInstance, result) -> float:
    """Score reply quality without pass@k context."""
    from .scorers.e2e import _judge_reply
    return _judge_reply(judge, inst.issue, result.final_reply)
