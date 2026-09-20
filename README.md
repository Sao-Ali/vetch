# Vetch

Vetch is a Kubernetes-native platform for virtual AI inference infrastructure.
It uses the Kubernetes API as its control plane, allowing inference capacity to
be described declaratively and maintained by controllers.

## Project vision

Vetch is intended to provide a portable way to create and operate virtual
inference clusters on Kubernetes. Users describe the capacity they want through
Kubernetes resources, and Vetch continuously reconciles the underlying compute,
accelerator interfaces, scheduling, and inference services needed to provide it.

The finished platform is expected to support:

- Declarative inference clusters managed through Kubernetes APIs.
- Virtualized compute nodes with predictable lifecycle and scaling behavior.
- Software-visible virtual accelerator properties for testing and development.
- Workload placement and scheduling across available inference capacity.
- CPU-backed inference for environments without dedicated accelerators.
- Kubernetes-native ownership, status, recovery, and cleanup semantics.

Vetch aims to model the interfaces and operational behavior that inference
software expects. It is not intended to emulate GPU hardware or reproduce the
performance of a physical accelerator.

## How it works

The `VirtualInferenceCluster` custom resource describes the desired number of
virtual inference nodes:

```yaml
apiVersion: infrastructure.vetch.io/v1alpha1
kind: VirtualInferenceCluster
metadata:
  name: demo
spec:
  nodes: 2
```

The Vetch controller watches these resources and reconciles their desired state.
The project is under active development, and virtual nodes are currently
represented by owned ConfigMaps while the controller lifecycle is established.
These placeholders do not run inference workloads.

```text
kubectl → Kubernetes API → VirtualInferenceCluster → Vetch controller → virtual nodes
```

## Local setup

Vetch supports a reproducible Nix development shell on Apple Silicon macOS and
x86_64 Linux. A manual toolchain remains supported as well.

Docker must be installed and running on the host for either setup. On macOS,
the Nix shell does not replace Docker Desktop or another Docker-compatible
virtual machine and daemon.

### Nix development environment

Install [Nix](https://nixos.org/download/) with flakes enabled, clone the
repository, and enter the development shell:

```bash
nix develop
```

The locked environment provides Go, gopls, kubectl, kind, Kustomize, Make, and
Git. Project-specific generation, lint, and envtest tools remain pinned and
managed by the Makefile.

Confirm the environment before continuing:

```bash
go version
kubectl version --client
kind version
kustomize version
```

### Manual prerequisites

- Go 1.26 or newer
- Docker
- [kind](https://kind.sigs.k8s.io/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- Make

### Run Vetch

Create a local Kubernetes cluster:

```bash
kind create cluster --name vetch-dev
```

Select the cluster and confirm the context before making changes:

```bash
kubectl config use-context kind-vetch-dev
kubectl config current-context
kubectl get nodes
```

Install the Vetch API and start the controller:

```bash
make install
make run
```

Leave the controller running. In another terminal, create the example inference
cluster:

```bash
kubectl apply -f config/samples/infrastructure_v1alpha1_virtualinferencecluster.yaml
kubectl get virtualinferencecluster demo -o yaml
kubectl get configmaps -l infrastructure.vetch.io/component=dummy-node
```

The example requests two virtual nodes. Change `spec.nodes` to scale the desired
capacity. Zero requests an empty cluster, while negative values are rejected by
the Kubernetes API.

Remove the example when finished:

```bash
kubectl delete virtualinferencecluster demo
```

Press Ctrl+C in the controller terminal to stop Vetch.
