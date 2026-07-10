"""Dimension ① — End-to-end output scoring (weight: 30%).

Sub-dimensions:
  a. SWE-bench pass@k (70 pts): apply patch → run FAIL_TO_PASS + PASS_TO_PASS
  b. Reply quality LLM-judge (30 pts): clarity, verification, diagnostics
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult
from ..datasets.swebench import SWEBenchInstance


@dataclass
class E2EScore:
    pass_k: float       # 0-70
    reply_quality: float # 0-30
    total: float         # 0-100
    detail: str = ""


def score_e2e(
    instance: SWEBenchInstance,
    result: AgentResult,
    llm_judge_model: str = "gpt-4o",
) -> E2EScore:
    """
    Score end-to-end performance for one instance.

    Steps:
      1. pass_k: check if patch was generated, apply to repo, run tests
      2. reply_quality: send final reply + issue to LLM judge
    """
    pk = _score_pass_k(instance, result)
    rq = _score_reply_quality(instance, result, llm_judge_model)
    return E2EScore(pass_k=pk, reply_quality=rq, total=pk + rq)


def _score_pass_k(instance: SWEBenchInstance, result: AgentResult) -> float:
    """Check if the agent produced a patch that passes the required tests."""
    # For now: check if final_reply or any tool output contains test-pass evidence.
    # Phase 2: apply actual patch via Docker sandbox and run tests.
    if not result.final_reply:
        return 0.0
    # Phase 2 will implement real test execution
    return _placeholder_pass_k(instance)


def _placeholder_pass_k(instance: SWEBenchInstance) -> float:
    """Placeholder — Phase 2 implements real Docker-based test execution."""
    # TODO: docker exec in sandbox, apply patch, run pytest
    return 0.0


def _score_reply_quality(
    instance: SWEBenchInstance,
    result: AgentResult,
    model: str,
) -> float:
    """LLM-as-judge: evaluate the quality of the agent's final reply."""
    if not result.final_reply:
        return 0.0

    # Phase 2: call GPT-4o with the scoring prompt
    # prompt = f"""Evaluate the agent's reply for fixing this GitHub issue...
    # Output JSON: {{"score": <0-30 int>, "reason": "<brief>"}}"""
    # score = call_openai(model, prompt)

    return _placeholder_reply_quality(result)


def _placeholder_reply_quality(result: AgentResult) -> float:
    """Placeholder heuristic until LLM judge is wired in Phase 2."""
    reply = result.final_reply
    score = 0.0
    # Very rough heuristic — LLM judge replaces this entirely
    if len(reply) > 50:
        score += 10  # has meaningful content
    if "error" not in reply.lower():
        score += 10  # doesn't end in error
    if any(kw in reply.lower() for kw in ["fix", "patch", "change", "modif"]):
        score += 10  # mentions what was done
    return min(score, 30.0)
