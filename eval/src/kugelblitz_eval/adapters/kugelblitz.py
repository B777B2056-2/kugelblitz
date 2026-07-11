"""Kugelblitz agent adapter — calls eval-cli via subprocess."""

import json
import subprocess
import time
from pathlib import Path
from .base import AgentAdapter, AgentResult, ToolCall, PlanUpdate


class KugelblitzAdapter(AgentAdapter):
    def __init__(self, cli_path: str = "./cli/eval-cli"):
        # Windows: try .exe extension if plain path doesn't exist
        p = Path(cli_path)
        if not p.exists() and not p.suffix:
            pex = p.with_suffix(".exe")
            if pex.exists():
                p = pex
        self.cli_path = p

    def name(self) -> str:
        return "Kugelblitz"

    def run(self, session_id: str, goal: str, workdir: str) -> AgentResult:
        start = time.monotonic()
        proc = subprocess.run(
            [str(self.cli_path), "run",
             "--session-id", session_id,
             "--goal", goal,
             "--workdir", workdir],
            capture_output=True, text=True, timeout=600,
        )
        elapsed = time.monotonic() - start
        exit_code = proc.returncode

        events = []
        for line in proc.stdout.strip().split("\n"):
            if line:
                try:
                    events.append(json.loads(line))
                except json.JSONDecodeError:
                    pass

        return self._parse(events, exit_code, elapsed)

    def _parse(self, events: list[dict], exit_code: int, elapsed: float) -> AgentResult:
        tool_calls = []
        plan_updates = []
        final_reply = ""
        tool_call_map: dict[str, ToolCall] = {}

        for evt in events:
            etype = evt.get("event", "")
            if etype == "tool_call":
                tc = ToolCall(
                    tool_name=evt.get("tool_name", ""),
                    args=evt.get("args", {}),
                )
                tool_calls.append(tc)
                tool_call_map[evt.get("tool_call_id", "")] = tc
            elif etype == "tool_result":
                tid = evt.get("tool_call_id", "")
                if tid in tool_call_map:
                    tool_call_map[tid].result = evt.get("output", {})
            elif etype == "reply_block":
                final_reply += evt.get("text", "")
            elif etype == "reply":
                final_reply += evt.get("text", "")
            elif etype == "plan_snapshot":
                plan_updates.append(PlanUpdate(
                    plan_id=evt.get("plan_id", ""),
                    tasks=evt.get("tasks", []),
                ))
            elif etype == "error":
                if not final_reply:
                    final_reply = f"Error: {evt.get('message', '')}"

        return AgentResult(
            final_reply=final_reply.strip(),
            tool_calls=tool_calls,
            plan_updates=plan_updates,
            exit_code=exit_code,
            duration_sec=elapsed,
        )
