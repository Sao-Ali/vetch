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
and cloud-init configuration for the declared model and worker count. KubeVirt
must already be installed in the cluster. The controller reports whether
KubeVirt has marked the current VM generation ready; this does not imply
running inference workers. The reported worker capacity is calculated from
the request. Existing `spec.nodes` resources continue to use ConfigMaps;
switching to VM mode removes those owned placeholders.

## Target system

The planned system separates infrastructure provisioning from prompt execution:

```text
Provisioning
User/CLI -> Kubernetes API -> Vetch Operator -> KubeVirt VM(s) -> Vetch Agent -> workers

Inference
User -> inference gateway -> capacity/health policy -> cloud worker or edge adapter
     <- model response + execution location + measurements -------------------
```

### Component responsibilities

- **AWS:** supplies cloud infrastructure. EC2 provides worker machines and EKS
  provides a managed Kubernetes control plane.
- **Kubernetes:** stores desired state and manages declared workloads and their
  lifecycle.
- **KubeVirt:** manages inference-server virtual machines through Kubernetes.
  One planned VM represents one inference server.
- **Vetch Operator:** reconciles Vetch custom resources into infrastructure.
- **Vetch Agent:** runs inside an inference VM, manages workers, enforces CPU and
  memory limits, and reports state and measurements.
- **Inference runtime:** performs model computation. A CPU-capable runtime such
  as `llama.cpp` is a candidate, subject to compatibility testing.
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

The first distribution policy sends each complete request to one backend. It
filters for healthy backends that support the model and have a free slot, then
chooses among eligible targets using queue length.

When capacity is exhausted, a request must enter a bounded queue or receive a
clear capacity error. Marking a worker or backend offline stops new assignments.
In-flight failures receive bounded retries or a clear error, and duplicate
attempts are recorded.

## Boundaries

Vetch manages inference infrastructure. It does not train a model, design a
chip, emulate GPU hardware, or claim that CPU workers reproduce dedicated
accelerator performance.

## References

- [Arduino VENTUNO Q](https://www.arduino.cc/product-ventuno-q)
- [KubeVirt virtual hardware](https://kubevirt.io/user-guide/compute/virtual_hardware/)
- [llama.cpp](https://github.com/ggml-org/llama.cpp)
