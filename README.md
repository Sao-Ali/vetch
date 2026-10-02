# Vetch

Vetch is a platform for provisioning and managing compute resources used to run
AI inference in the cloud and at the edge. The project begins with configurable,
CPU-backed inference workers in cloud virtual machines, then extends the same
workload interface to an Arduino VENTUNO Q for comparison and request routing.

The project focuses on infrastructure and resource management. It uses a small
open-source model as a real, reproducible workload; it does not train a model,
design a chip, emulate accelerator hardware, or claim CPU performance equivalent
to a GPU or purpose-built AI accelerator.

## Source of truth

The 2026–2027 **Vetch Full Proposal** and **Vetch Advisor Overview: Inference
Infrastructure** are the primary sources for product scope, architecture,
evaluation, and milestones. Their current content is consolidated in this
README so that the repository has a version-controlled project reference. The
source PDFs are kept local and ignored by Git.

When project materials disagree, use this order:

1. The two proposal documents for intended product direction and deliverables.
2. The checked-in API, controller, and tests for behavior that exists today.
3. Older design notes only for details that do not conflict with either source.

Planned components below are explicitly marked. They should be implemented
incrementally as their milestone begins, rather than treated as current
capabilities.

## Goals and scope

Vetch has three ordered goals:

1. **Cloud foundation:** provision virtual machines, allocate CPU-backed
   inference workers, enforce their resource limits, and run a real small model.
2. **Cloud/edge comparison:** run the same model and workload in the cloud and
   on the VENTUNO Q, then compare performance under documented conditions.
3. **Request-level distribution:** route each complete inference request to an
   available cloud or edge backend using a simple health and capacity policy.

Splitting a single model execution across cloud and edge is a stretch research
question, not a core deliverable. It should only be investigated after the
cloud foundation, controlled benchmarks, and whole-request routing work.

## Current implementation

The repository currently implements the Kubernetes control-plane foundation.
A namespaced `VirtualInferenceCluster` custom resource declares a desired node
count:

```yaml
apiVersion: infrastructure.vetch.io/v1alpha1
kind: VirtualInferenceCluster
metadata:
  name: demo
spec:
  nodes: 2
```

The Go controller reconciles that count into owned ConfigMaps that stand in for
virtual inference nodes. It creates, repairs, and removes those dummy nodes and
reports the result through the resource's `Available` condition.

```text
kubectl -> Kubernetes API -> VirtualInferenceCluster -> Vetch controller -> owned ConfigMaps
```

Changing `spec.nodes` explicitly scales the dummy nodes; this is desired-state
reconciliation, not autoscaling. ConfigMaps have deterministic names such as
`demo-node-0`, identifying labels, and controller OwnerReferences. The
controller watches its children and does not adopt or delete an unrelated
ConfigMap merely because its name or labels match. Kubernetes garbage
collection removes owned children after their parent is deleted.

These ConfigMaps do not provision a VM, enforce resources, or run inference.
KubeVirt, AWS, the Vetch Agent, an inference runtime, a gateway, the CLI, and
VENTUNO Q integration are target architecture and remain planned work.

## Target architecture

The planned system separates provisioning from the per-prompt inference path:

```text
Provisioning
User/CLI -> Kubernetes API -> Vetch Operator -> KubeVirt VM(s) -> Vetch Agent -> workers

Inference
User -> inference gateway -> capacity/health policy -> cloud worker or edge adapter
     <- model response + execution location + measurements -------------------
```

- **AWS** supplies cloud infrastructure; EC2 provides worker machines and EKS
  provides a managed Kubernetes control plane.
- **Kubernetes** stores desired state and manages declared workloads and their
  lifecycle.
- **KubeVirt** allows the operator to manage inference-server virtual machines
  through Kubernetes. One planned VM represents one inference server.
- **The Vetch Operator** reconciles Vetch custom resources into the required
  infrastructure.
- **The Vetch Agent** runs inside each inference VM, manages one or more
  workers, enforces CPU and memory limits, and reports state and measurements.
- **An existing inference runtime** performs model computation. A CPU-capable
  runtime such as `llama.cpp` is a candidate, subject to compatibility testing.
- **The inference gateway** accepts prompts, selects an eligible worker, and
  returns results. Generated tokens do not travel through the Kubernetes API.
- **The edge adapter** exposes compatible model, health, capacity, and result
  information from the VENTUNO Q. The board does not need to run KubeVirt or
  join the cloud Kubernetes cluster.

A standalone management REST API or database is not required for the initial
control plane. CLI names and commands remain illustrative until implemented.

## Worker resource model

A virtual accelerator initially means a logical inference worker with a real
CPU and memory budget. It is also described as a **CPU-backed inference
worker** to avoid implying hardware emulation.

| Property | Intended meaning |
| --- | --- |
| Worker count | Independently managed execution slots with real backing allocations; a larger count does not create physical capacity. |
| Memory | Per-worker RAM limit and admission budget for model weights, runtime overhead, and context memory. Unused budgets are not automatically pooled. |
| Compute | CPU quota and/or assigned cores plus explicit runtime thread settings, expressed as CPU units rather than GPU equivalents. |
| State | `PROVISIONING`, `READY`, `BUSY`, `OFFLINE`, or `ERROR`, controlling eligibility for new requests. |
| Bandwidth | Descriptive metadata initially; traffic shaping is a separately identified stretch feature. |

The initial multi-worker design uses separate model instances for independent
requests. It demonstrates allocation and concurrent execution, not one model
partitioned across multiple workers. Resource requests must eventually control
the real model process; recording capacity without enforcing it is insufficient.

## Request routing and failure behavior

The first distribution policy routes each request in full to one backend. It
filters for healthy backends that support the requested model and have a free
slot, then chooses among eligible targets using queue length. When capacity is
exhausted, a request must either enter a bounded queue or receive a clear
capacity error.

Marking a backend or worker offline stops new assignments. In-flight failures
receive bounded retries or a clear error, and duplicate attempts are recorded.
Evaluation compares this policy with fixed-backend routing under the same load.

## Evaluation plan

Cloud and edge tests should use the same model weights, prompts, generation
settings, and output-token limits wherever supported. Every result records the
precision, quantization, runtime version, CPU allocation, and actual execution
device. Required backend differences must be disclosed rather than described as
identical experiments.

Measurements include:

- end-to-end request latency and backend execution time;
- time to first token and generated tokens per second;
- median and tail latency across repeated trials and concurrency levels;
- peak memory and available compute-utilization counters;
- deployment and model-loading time, separating cold and warm runs;
- queue time, completed requests per second, failures, and assignments by
  backend; and
- cloud network time separated from model execution time.

Output samples establish that real inference ran, but answer quality and exact
text equality across runtimes are not primary comparisons. Power and energy are
optional when reliable instrumentation exists. AWS cost should be reported for
the tested configuration, not directly equated with a board's purchase price.

## Milestones

The cloud foundation is the minimum successful deliverable. Edge benchmarking
and routing follow it and depend on access to compatible hardware and runtimes.

### Repository foundation — complete

- Define `VirtualInferenceCluster` with explicit desired node count.
- Reconcile deterministic, owned dummy nodes idempotently.
- Support scale-up, scale-down, repair, status, ownership safety, and child
  watches with envtest coverage.
- Preserve Kubernetes as the control-plane API while later interfaces are
  designed.

Exit evidence: `spec.nodes` controls the set of owned ConfigMaps and the parent
reports `Available=True`. This validates controller lifecycle only, not
inference capacity.

### Quarter 1 — cloud foundation

1. **Feasibility validation:** select a small model and CPU runtime; verify
   model memory needs and local KubeVirt hardware-virtualization requirements.
2. **Real VM lifecycle:** extend the resource design and operator so declared
   capacity creates and updates KubeVirt inference VMs instead of placeholders.
3. **Agent and enforcement:** introduce the in-VM agent, worker states,
   admission behavior, and Linux CPU/memory enforcement.
4. **Single-worker inference:** launch the model inside its enforced boundary
   and return a real response with worker, backend, latency, and throughput.
5. **Multiple workers and availability:** run two independently limited workers,
   exercise concurrent requests, change a budget, and demonstrate that an
   `OFFLINE` worker receives no new assignments.

Quarter 1 succeeds when resource declarations control the real inference
process and changes to limits or availability affect admission predictably.

### Quarter 2 — cloud, edge, and routing

1. **AWS deployment:** validate compatible EC2 virtualization, choose a
   cost-conscious deployment size, and deploy the cloud foundation on EKS.
2. **VENTUNO Q integration:** run a compatible model through the common
   request/result interface and report whether execution uses CPU or supported
   specialized acceleration.
3. **Controlled benchmark:** collect comparable cloud and edge measurements
   with recorded configurations, repeated trials, cold/warm separation, and
   concurrency levels.
4. **Request-level routing:** distribute complete requests using model support,
   health, free slots, and queue length; demonstrate capacity exhaustion and
   backend unavailability behavior.
5. **Evaluation and final demo:** compare fixed and routed workloads, publish
   reproducible results, and document deployment, resource specifications, the
   agent interface, and the end-to-end workflow.

Quarter 2 succeeds when the same workload interface reaches cloud and edge and
routing decisions can be evaluated from measured results. Only then should the
team consider the model-partitioning stretch study.

## Final demonstration

The intended demonstration connects infrastructure state to visible model
output:

1. Request one cloud inference VM with two CPU-backed workers and explicit
   budgets; show reconciliation and readiness.
2. Send a prompt and return the model response with worker ID, backend, and
   latency/throughput measurements.
3. Send concurrent requests to both workers, then change a memory budget or
   mark one worker `OFFLINE` and show correct admission behavior.
4. Run the same controlled workload on VENTUNO Q and present cloud/edge results.
5. Route a request batch across both backends and show behavior when one becomes
   unavailable.

The visible result must include real model output and an execution record, not
only provisioned-resource metadata. No performance outcome is assumed in
advance.

## Local development

Vetch supports a reproducible Nix development shell on Apple Silicon macOS and
x86_64 Linux. Docker must be installed and running on the host. On macOS, Nix
does not replace Docker Desktop or another Docker-compatible VM and daemon.

### Nix environment

Install [Nix](https://nixos.org/download/) with flakes enabled, clone the
repository, and enter:

```bash
nix develop
```

The locked shell provides Go, gopls, kubectl, kind, Kustomize, Make, and Git.
The Makefile pins project-specific generation, lint, and envtest tools.

### Manual prerequisites

- Go 1.26 or newer
- Docker
- [kind](https://kind.sigs.k8s.io/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- Make

### Run the current controller

Create an isolated development cluster, select it, and confirm the context:

```bash
kind create cluster --name vetch-dev
kubectl config use-context kind-vetch-dev
kubectl config current-context
kubectl get nodes
```

Install the API and start the controller:

```bash
make install
make run
```

In another terminal, apply and inspect the sample:

```bash
kubectl apply -f config/samples/infrastructure_v1alpha1_virtualinferencecluster.yaml
kubectl get virtualinferencecluster demo -o yaml
kubectl get configmaps -l infrastructure.vetch.io/component=dummy-node
```

The sample requests two nodes. Edit `spec.nodes` to test explicit scale-up or
scale-down. Zero requests an empty cluster; the Kubernetes API rejects negative
values. Remove the sample with:

```bash
kubectl delete virtualinferencecluster demo
```

## Development and verification

```bash
make lint-config lint
make test
make build
```

Tests use Ginkgo/Gomega and envtest, which runs a temporary Kubernetes API
server and etcd independently of the development cluster. Envtest does not run
the Kubernetes garbage collector, so cascading deletion must be verified in an
isolated Kind cluster when needed.

After API fields or markers change, run `make manifests generate`. After Go
changes, run `make lint-fix` and `make test`.

## Repository map

| Path | Responsibility |
| --- | --- |
| `api/v1alpha1/` | Resource fields, registration, and generated copy methods |
| `cmd/main.go` | Controller-manager entry point |
| `internal/controller/` | Reconciliation and envtest tests |
| `config/crd/` | Generated CRD and packaging |
| `config/samples/` | Example cluster request |
| `config/rbac/` | Kubernetes permissions |
| `config/manager/`, `config/default/` | Controller deployment scaffolding |
| `Dockerfile`, `.dockerignore` | Operator container build |
| `Makefile` | Development, generation, and deployment commands |
| `.github/workflows/` | Lint and test CI |
| `skills/` | Repository-specific agent workflows |
| `PROJECT` | Kubebuilder metadata, managed with the Kubebuilder CLI |

Container deployment scaffolding is retained for later use; the currently
verified workflow runs locally and no published image is supplied.

## References

- [Arduino VENTUNO Q](https://www.arduino.cc/product-ventuno-q)
- [KubeVirt virtual hardware](https://kubevirt.io/user-guide/compute/virtual_hardware/)
- [llama.cpp](https://github.com/ggml-org/llama.cpp)
