"""Dimension ② — Tool trajectory scoring (weight: 30%).

Sub-dimensions:
  a. Tool selection correctness (40 pts): tools used match FSM state whitelist
  b. Tool efficiency (30 pts): steps-to-goal ratio
  c. Tool argument validity (30 pts): required args present and non-empty
"""

from dataclasses import dataclass

from ..adapters.base import AgentResult

# Expected tools per FSM state (mirrors runtime/engine/fsm/state.go)
EXPECTED_TOOLS: dict[str, list[str]] = {
    "intent":     ["set_work_mode"],
    "direct":     ["shell_exec", "web_fetch", "web_search", "file_read", "file_write",
                   "file_copy", "file_delete", "dir_create", "dir_copy",
                   "memory_store", "memory_search", "memory_get_section",
                   "memory_remove", "memory_list_sections", "memory_stats",
                   "skill_use", "ask_human"],
    "init":       ["plan_create", "task_insert", "memory_store", "memory_search",
                   "memory_get_section", "memory_remove", "memory_list_sections",
                   "memory_stats", "memory_extract", "skill_use"],
    "confirmed":  ["ask_human", "confirm_plan"],
    "doing":      ["task_query", "task_status_update"],
    "updating":   ["memory_store", "memory_search", "memory_get_section",
                   "memory_remove", "memory_list_sections", "memory_stats",
                   "memory_extract", "skill_use", "task_insert", "task_delete",
                   "task_query", "plan_query"],
}

# Required argument keys per tool (non-exhaustive)
REQUIRED_ARGS: dict[str, list[str]] = {
    "set_work_mode":       ["mode"],
    "plan_create":         ["name"],
    "task_insert":         ["goal"],
    "task_status_update":  ["id", "status"],
    "task_delete":         ["id"],
    "task_query":          ["id"],
    "confirm_plan":        ["status"],
    "plan_rollback":       ["plan_id", "target_version"],
    "file_read":           ["path"],
    "file_write":          ["path", "content"],
    "file_delete":         ["path"],
    "file_copy":           ["source", "destination"],
    "shell_exec":          ["command"],
    "memory_store":        ["section", "key", "value"],
    "memory_search":       ["query"],
    "memory_remove":       ["section", "key"],
    "web_search":          ["query"],
    "web_fetch":           ["url"],
    "ask_human":           ["question"],
}


@dataclass
class ToolScore:
    selection: float    # 0-40
    efficiency: float   # 0-30
    args_valid: float   # 0-30
    total: float        # 0-100
    detail: str = ""


def score_tools(result: AgentResult) -> ToolScore:
    sel = _score_selection(result)
    eff = _score_efficiency(result)
    args = _score_args(result)
    return ToolScore(selection=sel, efficiency=eff, args_valid=args, total=sel + eff + args)


def _score_selection(result: AgentResult) -> float:
    """Check if all tool calls are in the expected set for any state."""
    if not result.tool_calls:
        return 0.0

    all_allowed: set[str] = set()
    for tools in EXPECTED_TOOLS.values():
        all_allowed.update(tools)

    valid = sum(1 for tc in result.tool_calls if tc.tool_name in all_allowed)
    return (valid / len(result.tool_calls)) * 40.0


def _score_efficiency(result: AgentResult) -> float:
    """Score based on tool call count — fewer is better."""
    n = len(result.tool_calls)
    if n == 0:
        return 0.0
    if n <= 5:
        return 30.0
    if n <= 10:
        return 25.0
    if n <= 20:
        return 15.0
    if n <= 30:
        return 8.0
    return 3.0


def _score_args(result: AgentResult) -> float:
    """Check if required arguments are present and non-empty."""
    if not result.tool_calls:
        return 0.0

    total_checks = 0
    passed_checks = 0

    for tc in result.tool_calls:
        required = REQUIRED_ARGS.get(tc.tool_name)
        if not required:
            continue
        for key in required:
            total_checks += 1
            val = tc.args.get(key)
            if val is not None and val != "":
                passed_checks += 1

    if total_checks == 0:
        return 30.0  # no args to validate → full score
    return (passed_checks / total_checks) * 30.0
