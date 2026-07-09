"""Agent adapter abstract base class — enables cross-agent comparison."""

from abc import ABC, abstractmethod
from dataclasses import dataclass, field


@dataclass
class ToolCall:
    tool_name: str
    args: dict
    result: dict | None = None


@dataclass
class PlanUpdate:
    plan_id: str
    tasks: list[dict]


@dataclass
class AgentResult:
    """Single execution output. Every agent adapter must return this."""
    final_reply: str
    tool_calls: list[ToolCall] = field(default_factory=list)
    plan_updates: list[PlanUpdate] = field(default_factory=list)
    exit_code: int = 0
    duration_sec: float = 0.0
    extra: dict = field(default_factory=dict)


class AgentAdapter(ABC):
    """Abstract base for agent adapters under evaluation."""

    @abstractmethod
    def name(self) -> str:
        """Human-readable agent identifier for reporting."""

    @abstractmethod
    def run(self, session_id: str, goal: str, workdir: str) -> AgentResult:
        """Execute one agent task and return structured output."""
