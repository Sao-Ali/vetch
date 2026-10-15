# Vetch

Vetch is a Kubernetes-native platform for managing virtual AI inference
infrastructure. It uses the Kubernetes API as its control plane so inference
capacity can be declared, observed, and reconciled consistently.

The long-term system will manage CPU-backed inference workers in cloud virtual
machines, compare them with an Arduino VENTUNO Q edge device, and route complete
inference requests between healthy backends.

> **Project status:** Vetch reconciles legacy placeholder nodes as owned
> ConfigMaps and can request one owned KubeVirt VM when KubeVirt is installed.
> It does not run inference workloads yet.

## How it works

A `VirtualInferenceCluster` declares the desired number of virtual nodes:

```yaml
apiVersion: infrastructure.vetch.io/v1alpha1
kind: VirtualInferenceCluster
metadata:
  name: demo
spec:
  nodes: 2
```

The controller continuously reconciles the requested count and reports the
result through Kubernetes status conditions.

```text
kubectl -> Kubernetes API -> VirtualInferenceCluster -> Vetch controller -> owned ConfigMaps
```

Changing `spec.nodes` scales the placeholder nodes up or down. The controller
uses deterministic names and Kubernetes ownership, repairs managed objects, and
does not adopt unrelated ConfigMaps.

The API also accepts a VM request using `spec.vmCount`, `spec.workersPerVM`,
`spec.model`, and the guest settings in `spec.vm`. With KubeVirt installed,
`vmCount: 1` creates one owned KubeVirt VM; `vmCount: 0` removes it. The count is
currently limited to zero or one. The VM uses the configured containerDisk
image, guest CPU and memory, and cloud-init user data that writes the model and
worker count to `/etc/vetch/config.json` inside a cloud-init-capable guest.
This does not start inference workers. See the
[VM example](config/samples/infrastructure_v1alpha1_virtualinferencecluster_vm.yaml)
and [development guide](docs/development.md) for the KubeVirt prerequisite.

`Available=True` with reason `VMReady` means KubeVirt reports the current VM
generation ready; it does not mean inference is serving. The
`declaredWorkerCapacity` value is requested VM count times workers per VM, not
an observed worker count. Resources using only `spec.nodes` keep the legacy
ConfigMap behavior. Switching to VM mode removes the old owned placeholders.

## Quick start

You need Docker, [kind](https://kind.sigs.k8s.io/), kubectl, Make, and Go 1.26 or
newer. Alternatively, enter the reproducible development environment with
`nix develop`.

Create and select an isolated local cluster:

```bash
kind create cluster --name vetch-dev
kubectl config use-context kind-vetch-dev
kubectl config current-context
```

Install the custom resource and run the controller:

```bash
make install
make run
```

Keep the controller running. In another terminal, create and inspect the sample:

```bash
kubectl apply -f config/samples/infrastructure_v1alpha1_virtualinferencecluster.yaml
kubectl get virtualinferencecluster demo -o yaml
kubectl get configmaps -l infrastructure.vetch.io/component=dummy-node
```

The sample creates two placeholder nodes. Change `spec.nodes` to exercise
explicit scale-up and scale-down. Remove the sample with:

```bash
kubectl delete virtualinferencecluster demo
```

For environment details and troubleshooting, see the
[development guide](docs/development.md).

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for the branch and pull request process.

Run the standard checks before submitting a change:

```bash
make lint-config lint
make test
make build
```

After changing API fields or markers, also run `make manifests generate` and
include generated CRDs and DeepCopy code in the change.

## Documentation

- [Documentation overview](docs/README.md)
- [Architecture](docs/architecture.md)
- [Project roadmap](docs/roadmap.md)
- [Evaluation plan](docs/evaluation.md)
- [Development guide](docs/development.md)

Vetch manages inference infrastructure; it does not train models, design chips,
emulate GPU hardware, or claim CPU performance equivalent to dedicated
accelerators.
