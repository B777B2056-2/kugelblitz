"""Dimension ③ — Plan quality scoring (weight: 25%).

Real implementation:
  a. DAG correctness (0-40): DFS color-cycle detection, dangling parent check
  b. Granularity (0-30): GPT-4o judge on subtask decomposition
  c. Consistency (0-30): done/planned ratio from AgentResult
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult
from ..common.llm import LLMJudge


@dataclass
class PlanScore:
    dag_correct: float
    granularity: float
    consistency: float
    total: float
    detail: str = ""


def score_plan(result: AgentResult, judge: LLMJudge) -> PlanScore:
    dag = _score_dag(result)
    gran = _score_granularity(result, judge)
    cons = _score_consistency(result)
    return PlanScore(dag_correct=dag, granularity=gran, consistency=cons, total=dag + gran + cons)


# ── (a) DAG correctness ──

def _score_dag(result: AgentResult) -> float:
    """DFS color-cycle detection on task dependency graph."""
    if not result.plan_updates:
        return 0.0

    tasks = result.plan_updates[-1].tasks
    if not tasks:
        return 0.0

    task_ids = {t.get("id", "") for t in tasks}
    if "" in task_ids:
        return 0.0  # empty ID invalid

    # Build adjacency: task → [parents it depends on]
    graph: dict[str, list[str]] = {tid: [] for tid in task_ids}
    for t in tasks:
        tid = t.get("id", "")
        pid_raw = t.get("parent_task_id", "")
        if not pid_raw:
            continue
        for pid in pid_raw.split(","):
            pid = pid.strip()
            if pid == tid:
                return 0.0  # self-loop
            if pid not in task_ids:
                return 0.0  # dangling reference
            graph[tid].append(pid)

    # DFS cycle detection (white=0, gray=1, black=2)
    WHITE, GRAY, BLACK = 0, 1, 2
    color: dict[str, int] = {tid: WHITE for tid in task_ids}

    def dfs(node: str) -> bool:
        """Return True if a back-edge (cycle) is found."""
        color[node] = GRAY
        for parent in graph[node]:
            if color[parent] == GRAY:
                return True   # back edge → cycle
            if color[parent] == WHITE and dfs(parent):
                return True
        color[node] = BLACK
        return False

    for tid in task_ids:
        if color[tid] == WHITE and dfs(tid):
            return 0.0  # cycle detected

    return 40.0


# ── (b) Granularity — LLM judge ──

def _score_granularity(result: AgentResult, judge: LLMJudge) -> float:
    """GPT-4o evaluates whether subtasks are at the right level of detail."""
    if not result.plan_updates:
        return 0.0

    tasks = result.plan_updates[-1].tasks
    if not tasks:
        return 0.0

    task_list = "\n".join(
        f"- [{t.get('id','?')}] {t.get('goal','')} (parent: {t.get('parent_task_id','none')})"
        for t in tasks
    )

    prompt = f"""Evaluate the quality of this task decomposition for fixing a software issue:

TASKS:
{task_list}

Score 0-30 across:
1. Independence (10 pts): Each task is independently actionable and verifiable.
2. Granularity (10 pts): Tasks are neither too coarse nor too fine. 2-7 tasks is ideal.
3. Dependency (10 pts): Parent/child relationships are logical and necessary.

Output ONLY: {{"independence": <0-10>, "granularity": <0-10>, "dependency": <0-10>}}"""

    try:
        scores = judge.ask(prompt)
        return (scores.get("independence", 0) +
                scores.get("granularity", 0) +
                scores.get("dependency", 0))
    except Exception:
        return 10.0  # conservative fallback


# ── (c) Execution consistency ──

def _score_consistency(result: AgentResult) -> float:
    """Ratio of completed tasks to planned tasks."""
    if not result.plan_updates:
        return 0.0

    tasks = result.plan_updates[-1].tasks
    if not tasks:
        return 0.0

    done = sum(1 for t in tasks if t.get("status") == "done")
    return (done / len(tasks)) * 30.0
