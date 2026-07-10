"""Langfuse score reporter — writes four-dimension scores to Langfuse API.

Kugelblitz framework already creates the Trace via its Observer.
The eval-runner writes scores to the same trace using the instance_id as key.
"""

from dataclasses import dataclass

from ..scorers.e2e import E2EScore
from ..scorers.tools import ToolScore
from ..scorers.plan import PlanScore
from ..scorers.memory import MemoryScore


@dataclass
class InstanceScores:
    instance_id: str
    e2e: E2EScore
    tools: ToolScore
    plan: PlanScore
    memory: MemoryScore
    total: float
    grade: str


class LangfuseReporter:
    """Writes scores to Langfuse. If langfuse is unavailable (no API key),
    falls back to stdout-only reporting."""

    def __init__(self, public_key: str = "", secret_key: str = "", host: str = ""):
        self.enabled = bool(public_key and secret_key)
        if self.enabled:
            import langfuse
            self.client = langfuse.Langfuse(
                public_key=public_key, secret_key=secret_key, host=host)

    def report(self, scores: InstanceScores):
        if not self.enabled:
            return
        trace_id = f"swebench__{scores.instance_id}"
        for name, value in [
            ("e2e_output", scores.e2e.total),
            ("e2e_pass_k", scores.e2e.pass_k),
            ("e2e_reply_quality", scores.e2e.reply_quality),
            ("tool_trajectory", scores.tools.total),
            ("tool_selection", scores.tools.selection),
            ("tool_efficiency", scores.tools.efficiency),
            ("tool_args", scores.tools.args_valid),
            ("plan_quality", scores.plan.total),
            ("plan_dag", scores.plan.dag_correct),
            ("plan_granularity", scores.plan.granularity),
            ("plan_consistency", scores.plan.consistency),
            ("memory_continuity", scores.memory.total),
            ("overall", scores.total),
        ]:
            self.client.create_score(
                trace_id=trace_id, name=name, value=value,
            )
