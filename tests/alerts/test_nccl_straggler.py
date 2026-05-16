"""Unit tests for the NCCL straggler alert rule.

The rule under test is in `provisioning/alerting/ingero-gpu.yaml`
under uid `ingero-nccl-straggler-suspected`. Its expression is:

    (count(rate(gpu_nccl_collective_count{op_type="ncclAllReduce"}[5m])) >= 3)
      and
    (min by (instance) (rate(gpu_nccl_collective_count{op_type="ncclAllReduce"}[5m]))
       / on() group_left()
       avg(rate(gpu_nccl_collective_count{op_type="ncclAllReduce"}[5m])) < 0.5)

Running a full Prometheus engine to evaluate PromQL is overkill for
a single rule, so this harness re-implements the rule's logic in
Python and exercises it against four synthetic scenarios.

If the YAML is edited and the logic diverges from the Python mirror
here, the structural assertions in CI (count>=3 and <0.5 substrings)
catch the obvious break; this script catches the subtle ones (e.g.
swapping min for max, or dropping the count guard).
"""

import re
import statistics
import sys
import unittest
from pathlib import Path

import yaml

ALERT_YAML = Path(__file__).parent.parent.parent / "provisioning" / "alerting" / "ingero-gpu.yaml"
RULE_UID = "ingero-nccl-straggler-suspected"


def load_rule_expr() -> str:
    """Return the raw PromQL expression from the YAML."""
    doc = yaml.safe_load(ALERT_YAML.read_text())
    for group in doc["groups"]:
        for rule in group["rules"]:
            if rule["uid"] == RULE_UID:
                return rule["data"][0]["model"]["expr"]
    raise SystemExit(f"rule {RULE_UID} not found in {ALERT_YAML}")


def rule_fires(per_instance_rates: dict[str, float]) -> bool:
    """Python mirror of the rule.

    Returns True iff the rule WOULD fire given the per-instance
    AllReduce rate samples.
    """
    count = len(per_instance_rates)
    if count < 3:
        return False
    rates = list(per_instance_rates.values())
    minimum = min(rates)
    average = statistics.fmean(rates)
    if average == 0:
        return False
    return (minimum / average) < 0.5


class TestNcclStragglerRule(unittest.TestCase):

    def test_yaml_invariants(self):
        """YAML still has the two clauses the rule's correctness depends on."""
        expr = load_rule_expr()
        self.assertIn("count(rate(", expr, "rule lost the count-guard")
        self.assertIn(">= 3", expr, "rule lost the cluster-size guard")
        self.assertIn("< 0.5", expr, "rule lost the min/avg threshold")
        self.assertIn("min by (instance)", expr, "rule lost the per-instance min selector")
        self.assertIn("avg(", expr, "rule lost the cluster-average reducer")
        # Sanity: NOT a max() so a fast outlier does not falsely fire.
        self.assertNotRegex(
            re.sub(r"\s+", " ", expr),
            r"max by \(instance\).*/.*avg\(",
            "rule looks like max/avg, not min/avg; fast outliers would falsely fire",
        )

    def test_scenario_a_single_node(self):
        """1-node cluster: rule never fires (degenerate baseline)."""
        self.assertFalse(rule_fires({"node-1": 100.0}))

    def test_scenario_b_two_nodes_both_degraded(self):
        """2-node both equally degraded: rule never fires.

        Acceptable degenerate baseline: count < 3 so the rule skips
        regardless of the rate distribution.
        """
        self.assertFalse(rule_fires({"node-1": 10.0, "node-2": 10.0}))
        self.assertFalse(rule_fires({"node-1": 1.0, "node-2": 1.0}))

    def test_scenario_c_three_nodes_with_laggard(self):
        """3-node with one slow laggard: rule fires.

        Two healthy nodes at 100, one laggard at 20. Average = 73.3,
        min = 20, min/avg = 0.27 < 0.5. Fires.
        """
        self.assertTrue(rule_fires({"node-1": 100.0, "node-2": 100.0, "node-3": 20.0}))

    def test_scenario_d_three_nodes_with_fast_outlier(self):
        """3-node with one fast outlier: rule does NOT fire.

        Two healthy nodes at 100, one fast node at 300. Average = 166.7,
        min = 100, min/avg = 0.6 > 0.5. Does NOT fire. This is the
        case that a max/avg-shaped rule would incorrectly trigger on;
        the min/avg shape is the correct one.
        """
        self.assertFalse(rule_fires({"node-1": 100.0, "node-2": 100.0, "node-3": 300.0}))

    def test_three_nodes_with_severe_laggard(self):
        """Extra: 3-node with a severely slow laggard fires more easily.

        100, 100, 5 → avg ≈ 68.3, min = 5, ratio = 0.073.
        """
        self.assertTrue(rule_fires({"node-1": 100.0, "node-2": 100.0, "node-3": 5.0}))

    def test_three_nodes_marginal_laggard_does_not_fire(self):
        """Extra: 3-node with a marginal laggard (ratio > 0.5) does NOT fire.

        100, 100, 70 → avg = 90, min = 70, ratio = 0.778.
        """
        self.assertFalse(rule_fires({"node-1": 100.0, "node-2": 100.0, "node-3": 70.0}))


if __name__ == "__main__":
    sys.exit(unittest.main(verbosity=2))
