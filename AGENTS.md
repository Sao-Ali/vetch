# Vetch agent guide

## Scope and workflow

Vetch is a Kubernetes-based platform for virtual AI inference infrastructure.
VirtualInferenceCluster has spec.nodes and the controller reconciles owned
ConfigMaps as dummy nodes, including explicit scaling and status.

Do not introduce KubeVirt, inference runtimes,
virtual accelerators, a CLI, cloud infrastructure, databases, a REST backend,
a frontend, or monitoring systems until requested.
The Kubernetes API is the control-plane API.

Keep changes scoped, incremental, and verified. Document behavior changes and
the commands used to validate them.
Avoid speculative abstractions or dependencies. Respect planning-only requests.
Do not stage, commit, push, or deploy unless authorized.

## Layout and generation

- api/v1alpha1: API definitions and registration.
- cmd/main.go: controller-manager entry point.
- internal/controller: reconciliation and tests.
- config/samples: editable examples.
- skills: repository-specific agent workflows; keep each skill self-contained and
  validate changed skills with the skill-creator validator when available.
- config/crd/bases, config/rbac/role.yaml, config/webhook/manifests.yaml,
  and **/zz_generated.*.go are generated; never hand-edit them.
- PROJECT is managed by Kubebuilder; use the CLI to update it.
- Keep scaffold markers and the existing directory layout.
- Scaffold new APIs and webhooks with kubebuilder create.

## Implementation

- Reconciliation must be idempotent; read desired state on every call.
- Treat a missing parent as normal; return other errors for retry.
- Use structured logging with capitalized messages, no trailing periods,
  balanced key/value pairs, and no secrets.
- Use Kubernetes types such as metav1.Condition and metav1.Time.
- ConfigMap children belong in the parent's namespace, with deterministic
  names, labels, and controller OwnerReferences.
- Check ownership before changes; never adopt or delete unrelated objects
  merely because names or labels match.
- Watch owned children with Owns; do not use sleep-driven workflows.
- Re-fetch before updates and avoid unnecessary writes.
- Use finalizers only when external cleanup requires them; ConfigMap children
  can use Kubernetes garbage collection.
- Scope RBAC to the operations actually implemented.

## Verification

After API fields or markers change:

```bash
make manifests generate
```

After Go changes:

```bash
make lint-fix
make test
```

Use envtest for API/controller tests. It does not run the garbage collector;
verify garbage collection in an isolated Kind cluster when implemented.
Future end-to-end tests must use a dedicated test cluster, never a dev/prod cluster.

Local development uses kind-vetch-dev and make run. Confirm the Kubernetes
context before mutations. Repository-only cleanup must not alter the cluster.

## Repository hygiene

Keep generated CRDs and DeepCopy code with source for reviewable checkouts.
Keep local tools in ignored bin/ and coverage outputs out of Git.
Never commit kubeconfigs, private keys, tokens, environment secrets, or local
credential directories. Inspect candidate files without exposing secret values.
Preserve unrelated user changes.
