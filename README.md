# Vetch

Vetch is a Kubernetes-based platform for virtual AI inference infrastructure.

## Current state: Milestone 2

A namespaced `VirtualInferenceCluster` resource describes a desired node count.
The Go controller reconciles that count into owned ConfigMaps that stand in for
virtual inference nodes. It creates, repairs, and removes these dummy nodes and
reports the result through the resource's `Available` status condition.

```text
kubectl → Kubernetes API → VirtualInferenceCluster → Vetch controller → owned ConfigMaps
```

Changing `spec.nodes` explicitly scales the dummy nodes; this is desired-state
reconciliation, not autoscaling. ConfigMaps use deterministic names such as
`demo-node-0`, carry labels and controller OwnerReferences, and are watched by
the controller. An unrelated ConfigMap with a desired name is never adopted or
deleted. Deleting a parent lets Kubernetes garbage collection remove its children.

The dummy nodes do not run inference. Later milestones may replace them with real
compute resources and add scheduling and CPU-backed inference. Vetch will model
software-visible accelerator properties, not emulate GPU hardware or reproduce
GPU performance.

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

The sample requests `nodes: 2`. Look for `Reconciling VirtualInferenceCluster`
with `desiredNodes: 2` in the controller log, then inspect the result:

```bash
kubectl get configmaps -l infrastructure.vetch.io/component=dummy-node
kubectl get virtualinferencecluster demo -o yaml
```

The controller creates `demo-node-0` and `demo-node-1`, and the parent reports an
`Available=True` condition. Edit `spec.nodes` to exercise explicit scale-up and
scale-down.
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
independent of your development cluster. Envtest does not run Kubernetes garbage
collection, so deletion cascading must be verified separately in an isolated Kind
cluster before relying on an end-to-end garbage-collection check.

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
