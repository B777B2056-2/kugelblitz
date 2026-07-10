"""Dimension ③ — Plan quality scoring (weight: 25%).

Sub-dimensions:
  a. DAG topological correctness (40 pts): no cycles, no dangling deps
  b. Subtask granularity LLM-judge (30 pts): right level of detail
  c. Plan-execution consistency (30 pts): tasks done / tasks planned
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult, PlanUpdate


@dataclass
class PlanScore:
    dag_correct: float      # 0-40
    granularity: float      # 0-30
    consistency: float      # 0-30
    total: float            # 0-100
    detail: str = ""


def score_plan(result: AgentResult, llm_judge_model: str = "gpt-4o") -> PlanScore:
    dag = _score_dag(result)
    gran = _score_granularity(result, llm_judge_model)
    cons = _score_consistency(result)
    return PlanScore(dag_correct=dag, granularity=gran, consistency=cons, total=dag + gran + cons)


def _score_dag(result: AgentResult) -> float:
    """Check DAG validity: must have tasks, no dangling parent references."""
    if not result.plan_updates:
        return 0.0

    # Use the last plan snapshot
    last_plan = result.plan_updates[-1]
    tasks = last_plan.tasks
    if not tasks:
        return 0.0

    task_ids = {t.get("id", "") for t in tasks}

    # Check for dangling dependencies (parent_id not in task_ids)
    valid = True
    for t in tasks:
        parent_id = t.get("parent_task_id", "")
        if parent_id and parent_id not in task_ids:
            valid = False
            break

    # Basic cycle check: if any task depends on itself
    for t in tasks:
        if t.get("parent_task_id") == t.get("id"):
            valid = False
            break

    return 40.0 if valid else 0.0


def _score_granularity(result: AgentResult, model: str) -> float:
    """LLM-judge: are subtasks at the right level of detail?"""
    if not result.plan_updates:
        return 0.0

    tasks = result.plan_updates[-1].tasks
    if not tasks:
        return 0.0

    # Phase 2: LLM judge call
    return _placeholder_granularity(tasks)


def _placeholder_granularity(tasks: list[dict]) -> float:
    """Heuristic pending LLM judge integration."""
    n = len(tasks)
    if n == 0:
        return 0.0
    if 2 <= n <= 7:
        return 30.0  # ideal range
    if n <= 10:
        return 20.0
    if n <= 15:
        return 10.0
    return 5.0


def _score_consistency(result: AgentResult) -> float:
    """How many planned tasks were executed (have status updates)?"""
    if not result.plan_updates:
        return 0.0

    last_plan = result.plan_updates[-1]
    tasks = last_plan.tasks
    if not tasks:
        return 0.0

    done = sum(1 for t in tasks if t.get("status") == "done")
    return (done / len(tasks)) * 30.0
