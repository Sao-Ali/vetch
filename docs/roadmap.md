# Project roadmap

The cloud foundation is the minimum successful deliverable. Edge benchmarking
and routing follow it and depend on access to compatible hardware and runtimes.

## Repository foundation — complete

- Define `VirtualInferenceCluster` with an explicit desired node count.
- Reconcile deterministic, owned dummy nodes idempotently.
- Support scale-up, scale-down, repair, status, ownership safety, and child
  watches with envtest coverage.
- Preserve Kubernetes as the control-plane API while later interfaces are
  designed.

Exit evidence: `spec.nodes` controls the set of owned ConfigMaps and the parent
reports `Available=True`. This validates controller lifecycle, not inference
capacity.

## Quarter 1 — cloud foundation

1. **Feasibility validation:** select a small model and CPU runtime; verify
   model memory requirements and local KubeVirt hardware-virtualization support.
2. **Real VM lifecycle:** extend the resource design and operator so declared
   capacity creates and updates KubeVirt inference VMs instead of placeholders.
3. **Agent and enforcement:** introduce the in-VM agent, worker states,
   admission behavior, and Linux CPU and memory enforcement.
4. **Single-worker inference:** launch the model inside its enforced boundary
   and return a real response with worker, backend, latency, and throughput.
5. **Multiple workers and availability:** run two independently limited workers,
   exercise concurrent requests, change a budget, and demonstrate that an
   `OFFLINE` worker receives no new assignments.

Quarter 1 succeeds when resource declarations control the real inference
process and changes to limits or availability affect admission predictably.

## Quarter 2 — cloud, edge, and routing

1. **AWS deployment:** validate compatible EC2 virtualization, choose a
   cost-conscious deployment size, and deploy the cloud foundation on EKS.
2. **VENTUNO Q integration:** run a compatible model through the common
   request/result interface and report whether execution uses CPU or supported
   specialized acceleration.
3. **Controlled benchmark:** collect comparable cloud and edge measurements
   with recorded configurations, repeated trials, cold/warm separation, and
   multiple concurrency levels.
4. **Request-level routing:** distribute complete requests using model support,
   health, free slots, and queue length; demonstrate capacity exhaustion and
   backend unavailability behavior.
5. **Evaluation and final demo:** compare fixed and routed workloads, publish
   reproducible results, and document deployment, resource specifications, the
   agent interface, and the end-to-end workflow.

Quarter 2 succeeds when the same workload interface reaches cloud and edge and
routing decisions can be evaluated from measured results. Model partitioning
should only be considered after these milestones are complete.

## Final demonstration

The final demonstration connects infrastructure state to visible model output:

1. Request one cloud inference VM with two CPU-backed workers and explicit
   budgets; show reconciliation and readiness.
2. Send a prompt and return the model response with worker ID, backend, and
   latency and throughput measurements.
3. Send concurrent requests to both workers, then change a memory budget or
   mark one worker `OFFLINE` and show correct admission behavior.
4. Run the same controlled workload on the VENTUNO Q and present cloud/edge
   results.
5. Route a request batch across both backends and show behavior when one becomes
   unavailable.

The visible result must include real model output and an execution record, not
only provisioned-resource metadata. No performance result is assumed in advance.

## Deliverables

- Source code and deployment instructions
- Resource specification and agent interface
- Reproducible benchmark workload and measured results
- Documented end-to-end demonstration
