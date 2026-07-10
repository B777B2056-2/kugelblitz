"""Dimension ④ — Memory continuity scoring (weight: 15%).

Multi-turn evaluation: same session_id across N related issues in the same repo.

Real implementation:
  a. Context retention (0-50): GPT-4o judges whether round N references round 1
  b. LTM quality (0-50): GPT-4o judges extracted memory items against actual dialogue
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult
from ..common.llm import LLMJudge


@dataclass
class MemoryScore:
    context_retention: float
    ltm_quality: float
    total: float
    detail: str = ""


def score_memory_single_turn() -> MemoryScore:
    """Single-turn: memory dimension partially applicable — neutral score."""
    return MemoryScore(context_retention=25.0, ltm_quality=25.0, total=50.0,
                       detail="single-turn: neutral")


def score_memory_multi_turn(rounds: list[AgentResult], judge: LLMJudge) -> MemoryScore:
    """Multi-turn memory scoring with GPT-4o judge."""
    if len(rounds) < 2:
        return score_memory_single_turn()

    ctx = _score_context_retention(rounds, judge)
    ltm = _score_ltm_quality(rounds, judge)
    return MemoryScore(context_retention=ctx, ltm_quality=ltm, total=ctx + ltm)


# ── (a) Context retention ──

def _score_context_retention(rounds: list[AgentResult], judge: LLMJudge) -> float:
    """Judge whether later rounds reference findings, files, or fixes from round 1."""
    first_reply = rounds[0].final_reply[:3000]
    first_tools = _describe_tools(rounds[0])

    scores = []
    for i, r in enumerate(rounds[1:], start=2):
        prompt = f"""Evaluate whether round 2+ of an AI agent retained context from round 1.

ROUND 1 OUTPUT (files touched, findings):
{first_reply}

ROUND 1 TOOLS USED:
{first_tools}

ROUND {i} OUTPUT:
{r.final_reply[:3000]}

Score 0-50 based on:
- File awareness (20 pts): Does round {i} reference files modified in round 1?
- Finding reuse (20 pts): Does it build on round 1's discoveries?
- Error avoidance (10 pts): Does it avoid repeating mistakes from round 1?

Output ONLY: {{"file_awareness": <0-20>, "finding_reuse": <0-20>, "error_avoidance": <0-10>}}"""
        try:
            s = judge.ask(prompt)
            scores.append(s.get("file_awareness", 0) +
                         s.get("finding_reuse", 0) +
                         s.get("error_avoidance", 0))
        except Exception:
            scores.append(25.0)

    return sum(scores) / len(scores) if scores else 25.0


# ── (b) LTM quality ──

def _score_ltm_quality(rounds: list[AgentResult], judge: LLMJudge) -> float:
    """Judge long-term memories extracted by the agent across rounds."""
    # Collect all extracted memory evidence from tool results
    memory_items: list[str] = []
    for r in rounds:
        for tc in r.tool_calls:
            if tc.tool_name == "memory_store" and tc.result:
                section = tc.args.get("section", "?")
                key = tc.args.get("key", "?")
                value = tc.args.get("value", "")
                memory_items.append(f"[{section}] {key}: {value[:300]}")
            if tc.tool_name == "memory_extract" and tc.result:
                output = tc.result.get("output", "")
                if isinstance(output, dict):
                    output = str(output)
                memory_items.append(output[:500])

    if not memory_items:
        return 25.0  # no memories extracted → neutral

    items_text = "\n".join(f"{i+1}. {m}" for i, m in enumerate(memory_items))
    dialogue_summary = "\n---\n".join(
        f"Round {i+1} reply: {r.final_reply[:500]}" for i, r in enumerate(rounds)
    )

    prompt = f"""Evaluate the quality of long-term memories extracted by an AI agent.

ACTUAL DIALOGUE (ground truth):
{dialogue_summary[:3000]}

EXTRACTED MEMORIES:
{items_text[:3000]}

Score 0-50:
- Accuracy (25 pts): Are the memories factually consistent with the dialogue?
- Utility (15 pts): Would these memories help in future tasks?
- Non-redundancy (10 pts): Are there no obvious duplicates?

Output ONLY: {{"accuracy": <0-25>, "utility": <0-15>, "non_redundancy": <0-10>}}"""
    try:
        s = judge.ask(prompt)
        return (s.get("accuracy", 0) +
                s.get("utility", 0) +
                s.get("non_redundancy", 0))
    except Exception:
        return 25.0


def _describe_tools(result: AgentResult) -> str:
    """Summarize tool usage for the judge prompt."""
    lines = []
    for tc in result.tool_calls:
        name = tc.tool_name
        if name in ("file_read", "file_write", "shell_exec"):
            lines.append(f"- {name}: {tc.args}")
    return "\n".join(lines) if lines else "(no file/shell tools used)"
