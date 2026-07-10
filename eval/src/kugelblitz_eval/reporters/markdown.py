"""Markdown report generator."""

import statistics
from datetime import datetime

from .langfuse import InstanceScores


def generate_report(scores: list[InstanceScores], dataset_name: str,
                    agent_name: str, output_path: str) -> str:
    """Generate a Markdown evaluation report."""

    avg_e2e = statistics.mean(s.e2e.total for s in scores)
    avg_tools = statistics.mean(s.tools.total for s in scores)
    avg_plan = statistics.mean(s.plan.total for s in scores)
    avg_memory = statistics.mean(s.memory.total for s in scores)
    avg_total = statistics.mean(s.total for s in scores)
    grades = [s.grade for s in scores]
    grade_counts = {g: grades.count(g) for g in ["S", "A", "B", "C", "D"]}

    lines = [
        f"# Kugelblitz Evaluation Report",
        f"",
        f"**Agent**: {agent_name}  ",
        f"**Dataset**: {dataset_name}  ",
        f"**Instances**: {len(scores)}  ",
        f"**Generated**: {datetime.now().strftime('%Y-%m-%d %H:%M')}  ",
        f"",
        f"## Summary",
        f"",
        f"| Dimension | Weight | Avg Score |",
        f"|-----------|--------|-----------|",
        f"| ① End-to-End Output | 30% | {avg_e2e:.1f} |",
        f"| ② Tool Trajectory | 30% | {avg_tools:.1f} |",
        f"| ③ Plan Quality | 25% | {avg_plan:.1f} |",
        f"| ④ Memory Continuity | 15% | {avg_memory:.1f} |",
        f"| **Total** | **100%** | **{avg_total:.1f}** |",
        f"",
        f"## Grade Distribution",
        f"",
        f"| S (≥90) | A (75-89) | B (60-74) | C (45-59) | D (<45) |",
        f"|---------|-----------|-----------|-----------|---------|",
        f"| {grade_counts.get('S', 0)} | {grade_counts.get('A', 0)} | "
        f"{grade_counts.get('B', 0)} | {grade_counts.get('C', 0)} | {grade_counts.get('D', 0)} |",
        f"",
        f"## Per-Instance Results",
        f"",
        f"| Instance | E2E | Tools | Plan | Memory | Total | Grade |",
        f"|----------|-----|-------|------|--------|-------|-------|",
    ]

    for s in scores:
        lines.append(
            f"| {s.instance_id[:40]} | {s.e2e.total:.0f} | {s.tools.total:.0f} | "
            f"{s.plan.total:.0f} | {s.memory.total:.0f} | {s.total:.0f} | {s.grade} |"
        )

    lines.extend([
        "",
        "## Sub-Dimension Details",
        "",
        "### ① E2E Output",
        f"- Avg pass@k: {statistics.mean(s.e2e.pass_k for s in scores):.1f}/70",
        f"- Avg reply quality: {statistics.mean(s.e2e.reply_quality for s in scores):.1f}/30",
        "",
        "### ② Tool Trajectory",
        f"- Avg selection: {statistics.mean(s.tools.selection for s in scores):.1f}/40",
        f"- Avg efficiency: {statistics.mean(s.tools.efficiency for s in scores):.1f}/30",
        f"- Avg arg validity: {statistics.mean(s.tools.args_valid for s in scores):.1f}/30",
        "",
        "### ③ Plan Quality",
        f"- Avg DAG correctness: {statistics.mean(s.plan.dag_correct for s in scores):.1f}/40",
        f"- Avg granularity: {statistics.mean(s.plan.granularity for s in scores):.1f}/30",
        f"- Avg consistency: {statistics.mean(s.plan.consistency for s in scores):.1f}/30",
        "",
        "### ④ Memory Continuity",
        f"- Avg context retention: {statistics.mean(s.memory.context_retention for s in scores):.1f}/50",
        f"- Avg LTM quality: {statistics.mean(s.memory.ltm_quality for s in scores):.1f}/50",
    ])

    content = "\n".join(lines)
    with open(output_path, "w") as f:
        f.write(content)
    return content
