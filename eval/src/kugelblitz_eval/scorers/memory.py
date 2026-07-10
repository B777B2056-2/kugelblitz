"""Dimension ④ — Memory continuity scoring (weight: 15%).

Evaluated across multi-turn sessions (3 related issues from same repo).

Sub-dimensions:
  a. Cross-turn context retention (50 pts): does round N reference round 1's findings?
  b. Long-term memory quality (50 pts): are extracted memories accurate and useful?

Single-turn instances get a default score (no memory dimension applicable).
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult


@dataclass
class MemoryScore:
    context_retention: float  # 0-50
    ltm_quality: float        # 0-50
    total: float              # 0-100
    detail: str = ""


def score_memory_single_turn() -> MemoryScore:
    """Single-turn eval: memory dimension not applicable, return neutral score."""
    return MemoryScore(context_retention=25.0, ltm_quality=25.0, total=50.0,
                       detail="Single-turn: memory dimension partially applicable")


def score_memory_multi_turn(
    rounds: list[AgentResult],
    llm_judge_model: str = "gpt-4o",
) -> MemoryScore:
    """
    Multi-turn memory scoring.

    rounds[0] = session-1, rounds[1] = session-2 (same session_id), etc.
    The agent should retain context from earlier rounds.
    """
    if len(rounds) < 2:
        return score_memory_single_turn()

    ctx = _score_context_retention(rounds, llm_judge_model)
    ltm = _score_ltm_quality(rounds, llm_judge_model)
    return MemoryScore(context_retention=ctx, ltm_quality=ltm, total=ctx + ltm)


def _score_context_retention(rounds: list[AgentResult], model: str) -> float:
    """Check if later rounds reference findings/files from earlier rounds."""
    if len(rounds) < 2:
        return 25.0

    # Phase 3: LLM-judge
    # Compare round N's final_reply against round 1's final_reply
    # Does it mention the same files/modules/issues?
    return _placeholder_context_retention(rounds)


def _placeholder_context_retention(rounds: list[AgentResult]) -> float:
    """Heuristic: check if later round replies mention similar keywords."""
    if len(rounds) < 2:
        return 25.0

    # Extract key terms from first round
    first_words = set(rounds[0].final_reply.lower().split())
    score = 25.0  # baseline

    for r in rounds[1:]:
        later_words = set(r.final_reply.lower().split())
        overlap = first_words & later_words
        # Bonus for shared terminology (file names, function names, etc.)
        if len(overlap) > 10:
            score += 12.5

    return min(score, 50.0)


def _score_ltm_quality(rounds: list[AgentResult], model: str) -> float:
    """Evaluate quality of long-term memories extracted across rounds."""
    # Phase 3: check MEMORY.md after multi-turn session
    # LLM judge: are extracted memories accurate, non-redundant, useful?
    return _placeholder_ltm_quality()


def _placeholder_ltm_quality() -> float:
    return 25.0  # neutral until Phase 3 implements actual LTM inspection
