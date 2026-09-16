# Vetch

Vetch is a Kubernetes-based platform for virtual AI inference infrastructure.

## Current state: Milestone 1

A namespaced `VirtualInferenceCluster` resource describes a desired node count.
The Go controller watches these resources, reads them, and logs the requested count.
It does not create nodes or run inference yet.

```text
kubectl → Kubernetes API → VirtualInferenceCluster → Vetch controller → log
```

Next: owned ConfigMaps as dummy nodes, with creation, scaling, deletion, and status.
Later: a CLI, KubeVirt Linux VMs, userspace virtual accelerators, scheduling, and
CPU-backed inference. Vetch will model software-visible accelerator properties,
not emulate GPU hardware or reproduce GPU performance.

## Run locally

Prerequisites: Go 1.26 or newer, Docker running, kind, kubectl, and make.
Kubebuilder 4.15.0 scaffolded this project and is needed for future scaffolding.
Make downloads generation, lint, and test tools into the ignored `bin/` directory.

Create a development cluster if it does not already exist:

```bash
kind create cluster --name vetch-dev
```

Select the development context, install the API, and run the controller:

```bash
kubectl config use-context kind-vetch-dev
kubectl get nodes
make manifests generate
make install
kubectl explain virtualinferencecluster.spec.nodes
make run
```

In another terminal:

```bash
kubectl apply -f config/samples/infrastructure_v1alpha1_virtualinferencecluster.yaml
kubectl get virtualinferencecluster demo -o yaml
```

The sample requests `nodes: 2`. Look for `Observed VirtualInferenceCluster`
with `desiredNodes: 2` in the controller log.
Zero nodes is valid; negative counts are rejected.

Ctrl+C stops the local controller. The resource remains saved in Kubernetes.
Remove the sample with `kubectl delete virtualinferencecluster demo`.

## Development checks

```bash
make lint-config lint
make test
make build
```

Tests use Ginkgo/Gomega and envtest: a temporary Kubernetes API server and etcd,
independent of your development cluster. Deployment end-to-end tests are deferred
until there is node lifecycle behavior to exercise.

After API or RBAC marker changes, run `make manifests generate`.
After Go changes, run `make lint-fix test`.

## Repository map

| Path | Responsibility |
| --- | --- |
| `api/v1alpha1/` | Resource fields, registration, generated copy methods |
| `cmd/main.go` | Starts the controller manager |
| `internal/controller/` | Reconciliation and envtest tests |
| `config/crd/` | Generated CRD and packaging |
| `config/samples/` | Example cluster request |
| `config/rbac/` | Kubernetes permissions |
| `config/manager/`, `config/default/` | Container-deployment scaffolding |
| `Dockerfile`, `.dockerignore` | Operator container build |
| `Makefile` | Development and deployment commands |
| `.github/workflows/` | Lint and test CI |
| `skills/` | Repository-specific agent workflows for onboarding and change preparation |
| `.golangci.yml`, `.custom-gcl.yml` | Code checks and logging-check plugin |
| `hack/boilerplate.go.txt` | Generated-file license header |
| `PROJECT` | Kubebuilder metadata; managed by the CLI |
| `AGENTS.md` | Assistant workflow and project constraints |

Container deployment scaffolding is retained for later use; the current verified
workflow runs locally. No published image is supplied. Kubebuilder's authenticated
HTTPS metrics support is retained; no Prometheus installation is required.

## Before publishing

Keep kubeconfigs, credentials, private keys, environment files, binaries, and test
outputs out of Git. Common local paths are ignored, but ignore rules cannot detect
secrets embedded in source. Review candidate files and the staged diff before committing.
Never put credential values in logs, examples, or issue reports.

## Agent skills

The repository includes optional, tool-readable workflows under `skills/`:

- `explain-vetch` maps the current codebase and controller flow.
- `write-commit-message` drafts a Conventional Commit message from the actual changes.
- `verify-before-commit` runs the checks relevant to a candidate commit and reports
  passed, failed, and skipped checks.

These skills do not grant permission to stage, commit, push, deploy, or mutate a
Kubernetes cluster. Their `SKILL.md` files contain upstream source and license attribution.
