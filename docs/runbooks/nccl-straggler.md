# Runbook: NCCL straggler suspected

This page is the alert annotation link for the
`NCCL straggler suspected` rule that ships in
`provisioning/alerting/ingero-gpu.yaml`.

## What the alert means

A node in your distributed-training job is processing far fewer
NCCL `ncclAllReduce` events than the cluster average for the past
five minutes. The rule fires when both clauses below are true:

1. **Cluster size is at least 3 nodes.** Below 3 nodes the min /
   avg comparison is degenerate (with N=1 the two are identical;
   with N=2 a single laggard moves the average enough that ratios
   are noise). The rule deliberately skips these baselines.
2. **The slowest node is processing under 50% of the average
   AllReduce rate.** Stragglers manifest as low-rate nodes, not
   high-rate ones: the laggard is stuck at the NCCL barrier, while
   every fast peer is spinning waiting for it. A node that runs
   twice as many AllReduces as the cluster mean (a fast outlier)
   is NOT what this rule catches.

PromQL expression:

```
(count(rate(gpu_nccl_collective_count{op_type="ncclAllReduce"}[5m])) >= 3)
  and
(
  min by (instance) (rate(gpu_nccl_collective_count{op_type="ncclAllReduce"}[5m]))
  / on() group_left()
  avg(rate(gpu_nccl_collective_count{op_type="ncclAllReduce"}[5m]))
  < 0.5
)
```

The alert annotation reports the firing node via the standard
`{{ $labels.instance }}` template field.

## Triage

The firing node is the candidate straggler. Open these dashboards
in order:

1. **Ingero Cluster: NCCL stragglers** (grafana.com ID 25273).
   Confirms the rate divergence visually and shows the per-op-type
   AllReduce / AllGather / ReduceScatter rates side by side.
2. **Ingero Cluster: Per-Node Drilldown** (grafana.com ID 25276),
   scoped to the firing node. Skim the GPU memory / CUDA op
   profiler / data-movement panels for an obvious bottleneck.
3. **Ingero Cluster: Memcpy Bandwidth** (grafana.com ID 25274) on
   the same node. Rule out a data-pipeline bottleneck before
   blaming NCCL.

Typical root causes, in rough order of frequency:

- **Slow data loader.** The node is spending more wall time
  blocking on data I/O than on CUDA work. Memcpy h2d throughput
  on the node is low compared to the cluster mean. Profile the
  data-loader pipeline (worker count, prefetch depth, disk read
  rate). Single most common cause in our experience.
- **Throttling.** GPU is thermally / power throttled. Check the
  GPU throttle panels (`gpu_throttle_active`); if the
  `cause=thermal` or `cause=power` flag is sticky, address
  cooling / power delivery on the node.
- **Memory pressure.** GPU memory is at saturation; allocator
  pressure stalls launches. Check `gpu_memory_used_bytes /
  gpu_memory_total_bytes` and the memory fragmentation panels.
- **Co-tenant contention.** Another process on the same GPU is
  competing for SM / memory bandwidth. Look for unexpected PIDs
  in the per-process panels.
- **Hardware fault.** Driver counters report uncorrectable ECC
  errors or PCIe link-width / link-speed degradation. Cordon the
  node out of the job, run `nvidia-smi -q` and the cluster's
  hardware diagnostic, and replace if the fault is persistent.

## Tuning the rule

The 50% threshold and the 5-minute hold time are conservative
defaults. To tune for your workload:

- **Tighter detection (fires more readily):** raise the threshold
  to `< 0.7` so the rule fires when the laggard drops to 70% of
  the cluster mean. Useful when the average is already inflated by
  spin-wait time and a 50%-rate node has been impacting the job
  long enough that the average has dragged down too.
- **Looser detection:** lower the threshold to `< 0.3`. Reserves
  the alert for severe stragglers and reduces noise on jobs with
  intentionally heterogeneous nodes.
- **Larger cluster guard:** raise `count(...) >= 3` to `>= N` for
  your cluster size. With a 64-node training cluster, a single-
  node straggler may not move the average enough to trip the
  default; consider segmenting the alert by job rather than
  cluster-wide.

The richer `fleet.cluster.find_stragglers`-driven version of this
alert (which uses peer-relative MAD instead of mean ratios and
produces a structured row per straggler) is on the v0.2 roadmap;
it requires the Ingero datasource backend to implement Grafana's
alerting interface.

## Acknowledgement / escalation

Acknowledge the alert in your usual alerting channel. If the
straggler persists past the next training-job restart, drain the
node and replace it.
