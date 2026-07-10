"""Evaluation orchestrator — ties dataset, agent, sandbox, scorers together."""

from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path

from .adapters.base import AgentAdapter
from .common.docker import Sandbox
from .common.llm import LLMJudge
from .datasets.swebench import SWEBenchInstance, build_goal, load_instances
from .reporters.langfuse import InstanceScores, LangfuseReporter
from .reporters.markdown import generate_report
from .scorers.e2e import score_e2e
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

    def run(self, dataset_path: str, max_instances: int | None = None) -> list[InstanceScores]:
        instances = load_instances(dataset_path, max_instances)

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
        sandbox = Sandbox(inst.instance_id, inst.repo, inst.base_commit)
        try:
            workdir = sandbox.start()
            goal = build_goal(inst)
            session_id = f"eval__{inst.instance_id}"

            # ── Run agent ──
            result = self.adapter.run(session_id, goal, workdir)

            # ── Extract patch and run tests ──
            patch = Sandbox.extract_patch_from_result(result)
            test_results = {}
            if patch:
                test_results = sandbox.apply_patch_and_test(
                    patch, inst.fail_to_pass, inst.pass_to_pass)

            # ── Score ──
            e2e = score_e2e(inst, result, sandbox, self.judge)
            tools = score_tools(result)
            plan = score_plan(result, self.judge)
            memory = score_memory_single_turn()

            total = (e2e.total * self.w_e2e + tools.total * self.w_tools +
                     plan.total * self.w_plan + memory.total * self.w_memory)

            scores = InstanceScores(
                instance_id=inst.instance_id,
                e2e=e2e, tools=tools, plan=plan, memory=memory,
                total=total, grade=grade(total),
            )

            self.reporter.report(scores)
            return scores

        finally:
            sandbox.stop()

    def generate_report(self, scores: list[InstanceScores],
                        dataset_name: str, output_path: str = "report.md") -> str:
        return generate_report(scores, dataset_name, self.adapter.name(), output_path)
