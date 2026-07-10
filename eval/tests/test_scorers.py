"""Unit tests for scoring functions — no Docker or LLM dependency."""

import pytest
from kugelblitz_eval.adapters.base import AgentResult, ToolCall, PlanUpdate
from kugelblitz_eval.scorers.tools import score_tools
from kugelblitz_eval.scorers.plan import score_plan
from kugelblitz_eval.scorers.memory import score_memory_single_turn, score_memory_multi_turn


# ── Tool scoring ──

def make_tc(name, args=None, result=None):
    return ToolCall(tool_name=name, args=args or {}, result=result)


def test_tools_all_valid():
    r = AgentResult(final_reply="ok", tool_calls=[
        make_tc("set_work_mode", {"mode": "plan"}),
        make_tc("plan_create", {"name": "fix"}),
        make_tc("file_read", {"path": "/workspace/main.py"}),
    ])
    s = score_tools(r)
    assert s.selection == 40.0  # all in whitelist
    assert s.efficiency >= 25.0  # 3 calls
    assert s.args_valid == 30.0  # all required args present


def test_tools_invalid_selection():
    r = AgentResult(final_reply="ok", tool_calls=[
        make_tc("nonexistent_tool", {}),
    ])
    s = score_tools(r)
    assert s.selection == 0.0


def test_tools_missing_args():
    r = AgentResult(final_reply="ok", tool_calls=[
        make_tc("file_read", {}),  # missing required "path"
    ])
    s = score_tools(r)
    assert s.args_valid == 0.0


def test_tools_empty():
    r = AgentResult(final_reply="ok", tool_calls=[])
    s = score_tools(r)
    assert s.total == 0.0


# ── Plan scoring ──

def make_task(tid, goal="task", status="done", parent=""):
    return {"id": tid, "goal": goal, "status": status, "parent_task_id": parent}


def test_plan_dag_valid():
    r = AgentResult(final_reply="ok", plan_updates=[
        PlanUpdate(plan_id="p1", tasks=[
            make_task("t1", status="done"),
            make_task("t2", status="done", parent="t1"),
        ]),
    ])
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_plan(r, LLMJudge(model="gpt-4o"))  # judge only used for granularity
    assert s.dag_correct == 40.0  # valid DAG
    assert s.consistency == 30.0  # both done


def test_plan_dag_self_loop():
    r = AgentResult(final_reply="ok", plan_updates=[
        PlanUpdate(plan_id="p1", tasks=[
            make_task("t1", parent="t1"),  # self-loop
        ]),
    ])
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_plan(r, LLMJudge(model="gpt-4o"))
    assert s.dag_correct == 0.0


def test_plan_dag_dangling():
    r = AgentResult(final_reply="ok", plan_updates=[
        PlanUpdate(plan_id="p1", tasks=[
            make_task("t1", parent="nonexistent"),
        ]),
    ])
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_plan(r, LLMJudge(model="gpt-4o"))
    assert s.dag_correct == 0.0


def test_plan_dag_cycle():
    r = AgentResult(final_reply="ok", plan_updates=[
        PlanUpdate(plan_id="p1", tasks=[
            make_task("t1", parent="t2"),
            make_task("t2", parent="t1"),  # cycle t1 → t2 → t1
        ]),
    ])
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_plan(r, LLMJudge(model="gpt-4o"))
    assert s.dag_correct == 0.0


def test_plan_empty():
    r = AgentResult(final_reply="ok", plan_updates=[])
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_plan(r, LLMJudge(model="gpt-4o"))
    assert s.total == 0.0


def test_plan_consistency():
    r = AgentResult(final_reply="ok", plan_updates=[
        PlanUpdate(plan_id="p1", tasks=[
            make_task("t1", status="done"),
            make_task("t2", status="pending"),
            make_task("t3", status="doing"),
            make_task("t4", status="failed"),
        ]),
    ])
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_plan(r, LLMJudge(model="gpt-4o"))
    assert s.consistency == 7.5  # 1 done / 4 total * 30


# ── Memory scoring ──

def test_memory_single_turn():
    s = score_memory_single_turn()
    assert s.total == 50.0


def test_memory_multi_turn_basic():
    r1 = AgentResult(final_reply="Modified main.py and utils.py. Root cause was a race condition in the cache module.")
    r2 = AgentResult(final_reply="The fix in main.py needs adjustment. The cache issue from round 1 also affects utils.py.")
    from kugelblitz_eval.common.llm import LLMJudge
    s = score_memory_multi_turn([r1, r2], LLMJudge(model="gpt-4o"))
    assert s.context_retention >= 0.0
    assert s.ltm_quality >= 0.0


# ── AgentResult parsing ──

def test_kugelblitz_adapter_parse():
    from kugelblitz_eval.adapters.kugelblitz import KugelblitzAdapter
    adapter = KugelblitzAdapter()
    events = [
        {"event": "reply", "text": "Let me analyze this."},
        {"event": "tool_call", "tool_call_id": "tc-1", "tool_name": "file_read",
         "args": {"path": "main.py"}},
        {"event": "tool_result", "tool_call_id": "tc-1", "tool_name": "file_read",
         "output": {"content": "print('hello')"}},
        {"event": "reply", "text": "The file contains a print statement."},
        {"event": "plan_snapshot", "plan_id": "plan-1", "tasks": [
            {"id": "t1", "goal": "fix bug", "status": "done"}]},
        {"event": "done", "session_id": "eval__test"},
    ]
    result = adapter._parse(events, exit_code=0, elapsed=1.0)
    assert result.exit_code == 0
    assert "Let me analyze" in result.final_reply
    assert "The file contains" in result.final_reply
    assert len(result.tool_calls) == 1
    assert result.tool_calls[0].tool_name == "file_read"
    assert result.tool_calls[0].args == {"path": "main.py"}
    assert result.tool_calls[0].result == {"content": "print('hello')"}
    assert len(result.plan_updates) == 1
    assert result.plan_updates[0].tasks[0]["id"] == "t1"
