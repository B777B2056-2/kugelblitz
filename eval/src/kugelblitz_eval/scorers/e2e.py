"""Dimension ① — End-to-end output scoring (weight: 30%).

Real implementation:
  a. pass@k (0-70): extract patch → apply in Docker → run pytest
  b. reply quality (0-30): GPT-4o structured judge
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult
from ..common.docker import Sandbox
from ..common.llm import LLMJudge


@dataclass
class E2EScore:
    pass_k: float
    reply_quality: float
    total: float
    detail: str = ""


def score_e2e(
    instance,           # SWEBenchInstance
    result: AgentResult,
    sandbox: Sandbox,
    judge: LLMJudge,
) -> E2EScore:
    patch = Sandbox.extract_patch_from_result(result)

    # (a) pass@k
    if patch:
        test_results = sandbox.apply_patch_and_test(
            patch, instance.fail_to_pass, instance.pass_to_pass)
        ftp_ratio = (test_results["fail_to_pass_passed"] /
                     max(test_results["fail_to_pass_total"], 1))
        ptp_ratio = (test_results["pass_to_pass_passed"] /
                     max(test_results["pass_to_pass_total"], 1))
        # FAIL_TO_PASS must all pass; PASS_TO_PASS must not regress
        pk = ftp_ratio * 50.0 + ptp_ratio * 20.0
    else:
        pk = 0.0
        test_results = {"error": "no patch found in agent output"}

    # (b) reply quality — GPT-4o judge
    rq = _judge_reply(judge, instance.issue, result.final_reply)

    detail = f"patch={bool(patch)} ftp_ratio={pk/70:.2f} reply={rq:.0f}"
    return E2EScore(pass_k=pk, reply_quality=rq, total=pk + rq, detail=detail)


def _judge_reply(judge: LLMJudge, issue: str, reply: str) -> float:
    """GPT-4o evaluates agent reply: clarity + verification + diagnostics."""
    if not reply.strip():
        return 0.0

    prompt = f"""You are evaluating an AI agent that was asked to fix a GitHub issue.

ISSUE:
{issue[:3000]}

AGENT FINAL REPLY:
{reply[:3000]}

Score the reply 0-30 across three dimensions (10 points each):
1. Clarity: Does it clearly explain what was changed and why?
2. Verification: Does it mention test results or how the fix was verified?
3. Diagnostics: If the fix failed, does it provide debugging information?

Output ONLY a JSON object: {{"clarity": <0-10>, "verification": <0-10>, "diagnostics": <0-10>}}"""

    try:
        scores = judge.ask(prompt)
        return (scores.get("clarity", 0) +
                scores.get("verification", 0) +
                scores.get("diagnostics", 0))
    except Exception:
        return 0.0
