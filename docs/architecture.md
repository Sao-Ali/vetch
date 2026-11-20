# Architecture

Vetch is designed to provision and manage compute resources for AI inference in
the cloud and at the edge. Kubernetes is the control-plane API. A small
open-source model provides a real, reproducible workload for the infrastructure.

## Goals

Vetch has three ordered goals:

1. Build a cloud foundation that provisions virtual machines, allocates
   CPU-backed inference workers, enforces resource limits, and runs a real model.
2. Run the same model and workload in the cloud and on an Arduino VENTUNO Q for
   controlled comparison.
3. Route complete inference requests between available cloud and edge backends
   using health and capacity information.

Splitting one model execution across cloud and edge is a stretch research topic,
not a core deliverable.

## Current implementation

The repository implements a namespaced `VirtualInferenceCluster` and its
controller. In legacy mode, `spec.nodes` declares a desired placeholder count;
the controller creates, repairs, scales, and removes deterministic, owned
ConfigMaps. In VM mode, `spec.vmCount: 1` requests one KubeVirt VM.

```text
kubectl -> Kubernetes API -> VirtualInferenceCluster -> Vetch controller
                                                       -> owned ConfigMaps (legacy)
                                                       -> owned KubeVirt VM (VM mode)
```

The ConfigMaps validate controller lifecycle and ownership behavior only. They
do not provision virtual machines, enforce resources, or run inference.

The API accepts a VM template with a count of zero or one, workers per VM, a
model identifier, guest image, guest CPU, and guest memory. In VM mode, the
controller reconciles one owned KubeVirt VirtualMachine with a containerDisk
and boot configuration. Without `spec.worker`, cloud-init supplies only the
declared model and worker count for VM boot checks. KubeVirt
must already be installed in the cluster. The controller reports whether
KubeVirt has marked the current VM generation ready; this does not imply
running inference workers. The reported worker capacity is calculated from
the request in `declaredWorkerCapacity`; inference mode additionally reports
observed `readyWorkers`. Existing `spec.nodes` resources continue to use ConfigMaps;
switching to VM mode removes those owned placeholders.

## Implemented local inference path

When `spec.worker` is present, the controller creates an owned agent Service,
an immutable bootstrap Secret, and an ingress NetworkPolicy for the VM. The
Secret supplies cloud-init userdata containing a private configuration
credential. The VM spec contains the Secret reference, not its contents.

The non-root agent validates model checksums and aggregate resource budgets,
starts CPU-only llama.cpp processes inside separate cgroup-v2 CPU/RAM limits,
and advances the applied generation after all requested models load. It reports
PROVISIONING, READY, BUSY, OFFLINE, and ERROR; failed workers retry after a
backoff during configuration reconciliation. Worker changes use the agent's
HTTP interface. VM hardware/image changes still require a VM restart.

The controller polls the agent with bounded timeouts and reports VM readiness,
reachability, applied configuration, and worker readiness separately. The
gateway filters for current model/generation and fresh observations, then lets
the agent atomically admit a request to a free worker. Returned metadata
includes model, backend, worker, device, token counts, and runtime duration.

Configuration writes require the private bootstrap credential. Ingress is
limited to controller pods in the configured controller namespace and gateway
pods in the resource's namespace when the CNI enforces NetworkPolicy. Read and
inference endpoints do not implement authentication or TLS yet. Keep these
services on trusted private networks; authenticated/encrypted edge transport
remains follow-up work. Rebuilding the guest is required to deploy agent changes.

## Planned edge extension

The local implementation already separates infrastructure provisioning from
prompt execution. The planned extension adds physical edge backends:

```text
Provisioning
YAML/client -> Kubernetes API -> Vetch Operator -> KubeVirt VM -> Vetch Agent -> workers

Inference
User -> inference gateway -> capacity/health policy -> cloud worker or edge adapter
     <- model response + execution location + measurements -------------------
```

### Component responsibilities

- **Central machine:** hosts Kubernetes, the Vetch controller/gateway, and local
  KubeVirt inference capacity. AWS is optional future deployment work.
- **Kubernetes:** stores desired state and manages declared workloads and their
  lifecycle.
- **KubeVirt:** manages inference-server virtual machines through Kubernetes.
  Each managed VM represents one inference server.
- **Vetch Operator:** reconciles Vetch custom resources into infrastructure.
- **Vetch Agent:** runs inside the inference VM, manages workers, enforces CPU and
  memory limits, and reports state. Native ARM64 board packaging is planned.
- **Inference runtime:** llama.cpp currently performs local CPU model computation.
- **Inference gateway:** accepts prompts, selects an eligible worker, and
  returns results. Generated tokens do not travel through the Kubernetes API.
- **Edge adapter:** reports supported models, health, capacity, and results from
  the VENTUNO Q. The board does not need to join the Kubernetes cluster.

A standalone management REST API or database is not required for the initial
control plane. CLI names and commands remain illustrative until implemented.

## Worker resource model

A virtual accelerator initially means a logical inference worker backed by a
real CPU and memory budget. The term **CPU-backed inference worker** is preferred
when hardware-emulation claims could otherwise be implied.

| Property | Intended meaning |
| --- | --- |
| Worker count | Independently managed execution slots with real backing allocations; increasing the count does not create physical capacity. |
| Memory | Per-worker RAM limit and admission budget for model weights, runtime overhead, and context memory. Unused budgets are not automatically pooled. |
| Compute | CPU quota and/or assigned cores with explicit runtime thread settings, expressed as CPU units rather than GPU equivalents. |
| State | `PROVISIONING`, `READY`, `BUSY`, `OFFLINE`, or `ERROR`; state determines eligibility for new work. |
| Bandwidth | Descriptive metadata initially; traffic shaping is a separate stretch feature. |

The initial multi-worker design uses separate model instances for independent
requests. It demonstrates allocation and concurrency, not model partitioning.
Resource requests must control the real model process; recording capacity
without enforcing it does not meet the project goal.

## Routing and failure behavior

The current distribution policy sends each complete request to a fresh,
healthy backend serving the requested model. Loaded-worker observations include
BUSY workers, so admission is enforced by the selected agent. If that agent
explicitly returns HTTP 503 with `capacity_exhausted`, the gateway can try
another eligible backend with the original request ID, model, and deadline.

When capacity is exhausted, a request must enter a bounded queue or receive a
clear capacity error. Marking a worker or backend offline stops new assignments.
Transport errors, other failures, and invalid responses return a clear error
without retrying a request that might already have executed. Queueing, automatic
edge selection, and latency-based routing remain planned.

## Boundaries

Vetch manages inference infrastructure. It does not train a model, design a
chip, emulate GPU hardware, or claim that CPU workers reproduce dedicated
accelerator performance.

## References

- [Arduino VENTUNO Q](https://www.arduino.cc/product-ventuno-q)
- [KubeVirt virtual hardware](https://kubevirt.io/user-guide/compute/virtual_hardware/)
- [llama.cpp](https://github.com/ggml-org/llama.cpp)
