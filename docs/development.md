# Development guide

This guide expands on the quick start in the root README.

## Environment

Vetch supports a reproducible Nix development shell on Apple Silicon macOS and
x86_64 Linux. Docker must be installed and running on the host. On macOS, Nix
does not replace Docker Desktop or another Docker-compatible VM and daemon.

Install [Nix](https://nixos.org/download/) with flakes enabled and enter:

```bash
nix develop
```

The locked shell provides Go, gopls, kubectl, kind, Kustomize, Make, and Git.
The Makefile pins project-specific generation, lint, and envtest tools.

Without Nix, install these prerequisites manually:

- Go 1.26 or newer
- Docker
- [kind](https://kind.sigs.k8s.io/)
- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- Make

## Run locally

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
values.

Remove the sample when finished:

```bash
kubectl delete virtualinferencecluster demo
```

## Verification

Run the repository checks:

```bash
make lint-config lint
make test
make build
```

Tests use Ginkgo/Gomega and envtest, which runs a temporary Kubernetes API
server and etcd independently of the development cluster. Envtest does not run
the Kubernetes garbage collector, so cascading deletion must be verified in an
isolated Kind cluster when needed.

After API fields or markers change, run:

```bash
make manifests generate
```

After Go changes, run:

```bash
make lint-fix
make test
```

## Repository layout

| Path | Responsibility |
| --- | --- |
| `api/v1alpha1/` | Resource fields, registration, and generated copy methods |
| `cmd/main.go` | Controller-manager entry point |
| `internal/controller/` | Reconciliation and envtest tests |
| `config/crd/` | Generated CRD and packaging |
| `config/samples/` | Example cluster request |
| `config/rbac/` | Kubernetes permissions |
| `config/manager/`, `config/default/` | Controller deployment scaffolding |
| `docs/` | Architecture, roadmap, evaluation, and development guidance |
| `Dockerfile`, `.dockerignore` | Operator container build |
| `Makefile` | Development, generation, and deployment commands |
| `.github/workflows/` | Lint and test CI |
| `skills/` | Repository-specific agent workflows |
| `PROJECT` | Kubebuilder metadata, managed with the Kubebuilder CLI |

Container deployment scaffolding is retained for later use. The currently
verified workflow runs locally, and no published image is supplied.
